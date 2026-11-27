package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dbmanager/internal/engine"
	"dbmanager/internal/engine/mssql"
	"dbmanager/internal/engine/postgres"
	"dbmanager/internal/store"
	"dbmanager/internal/validate"
)

// El "usuario root" es el administrador total (sysadmin / SUPERUSER) con el que DB Manager se conecta a
// cada servidor. Tenerlo propio evita depender de sa/postgres o de logins de aplicaciones sin permisos.

// ValidateRootUser valida el nombre del usuario root para el motor.
func ValidateRootUser(engineName, name string) error {
	if name == "" {
		return errors.New("el usuario root es obligatorio")
	}
	if err := validate.Identifier(engineName, name); err != nil {
		return fmt.Errorf("usuario root: %w", err)
	}
	if (engineName == validate.EngineMSSQL && mssql.IsSystemLogin(name)) ||
		(engineName == validate.EnginePostgres && postgres.IsSystemRole(name)) {
		return fmt.Errorf("%q es un usuario del sistema; elija otro nombre para el usuario root", name)
	}
	return nil
}

// adminEngine abre un engine con las credenciales dadas y comprueba que sean de administrador total.
func adminEngine(ctx context.Context, engineName string, c engine.Conn) (engine.Engine, error) {
	e, err := NewEngine(engineName, c)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := e.Ping(pctx); err != nil {
		e.Close()
		return nil, fmt.Errorf("no se pudo conectar con el usuario %q: %s", c.User, CleanErr(err))
	}
	ok, err := e.IsAdmin(pctx)
	if err != nil {
		e.Close()
		return nil, err
	}
	if !ok {
		e.Close()
		return nil, fmt.Errorf("el usuario %q no es administrador del servidor (%s)", c.User, adminRoleName(engineName))
	}
	return e, nil
}

func adminRoleName(engineName string) string {
	if engineName == validate.EngineMSSQL {
		return "sysadmin"
	}
	return "SUPERUSER"
}

// verifyAdmin comprueba que sv.AdminUser con password conecta y es administrador.
func (s *Services) verifyAdmin(ctx context.Context, sv store.Server, password string) error {
	e, err := adminEngine(ctx, sv.Engine, connWith(sv, sv.AdminUser, password))
	if err != nil {
		return err
	}
	return e.Close()
}

// RootStatus describe el usuario con el que DB Manager administra un servidor.
type RootStatus struct {
	User      string
	Reachable bool
	IsAdmin   bool
	Message   string
}

// RootStatus consulta si el usuario guardado conecta y tiene permisos de administrador.
func (s *Services) RootStatus(ctx context.Context, sv store.Server) RootStatus {
	st := RootStatus{User: sv.AdminUser}
	if sv.Mode == "local" {
		if err := s.requireRunning(ctx, sv); err != nil {
			st.Message = err.Error()
			return st
		}
	}
	e, err := s.Engine(sv)
	if err != nil {
		st.Message = err.Error()
		return st
	}
	defer e.Close()
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := e.Ping(cctx); err != nil {
		st.Message = "No se pudo conectar: " + CleanErr(err)
		return st
	}
	st.Reachable = true
	ok, err := e.IsAdmin(cctx)
	if err != nil {
		st.Message = "No se pudieron consultar los permisos: " + CleanErr(err)
		return st
	}
	st.IsAdmin = ok
	if ok {
		st.Message = fmt.Sprintf("El usuario %q es administrador (%s): DB Manager puede gestionar usuarios, bases y permisos.", sv.AdminUser, adminRoleName(sv.Engine))
	} else {
		st.Message = fmt.Sprintf("El usuario %q no es administrador (%s): no podrá crear usuarios ni cambiar permisos. Configure un usuario root.", sv.AdminUser, adminRoleName(sv.Engine))
	}
	return st
}

// ConfigureRootRequest cambia (o crea) el usuario root de un servidor.
type ConfigureRootRequest struct {
	User              string // vacío = conservar el actual
	Password          string // vacío = conservar la actual (solo si el usuario no cambia)
	BootstrapUser     string // opcional: administrador (p. ej. sa) con el que crear el root si el actual no tiene permisos
	BootstrapPassword string
	DropPrevious      bool // eliminar el login anterior del servidor al cambiar de usuario
}

// ConfigureRoot crea o actualiza el usuario root en el servidor, verifica que funcione y lo guarda.
// Devuelve el servidor actualizado y un aviso (no fatal) si no se pudo eliminar el login anterior.
func (s *Services) ConfigureRoot(ctx context.Context, sv store.Server, r ConfigureRootRequest) (store.Server, string, error) {
	user := strings.TrimSpace(r.User)
	if user == "" {
		user = sv.AdminUser
	}
	if err := ValidateRootUser(sv.Engine, user); err != nil {
		return store.Server{}, "", err
	}
	sameUser := strings.EqualFold(user, sv.AdminUser)
	stored, err := s.Store.ServerPassword(sv)
	if err != nil {
		return store.Server{}, "", err
	}
	pw := r.Password
	if pw == "" {
		if !sameUser {
			return store.Server{}, "", errors.New("indique la contraseña del nuevo usuario root")
		}
		pw = stored
	}
	if pw == "" {
		return store.Server{}, "", errors.New("la contraseña del usuario root es obligatoria")
	}
	if sv.Mode == "local" {
		if err := s.requireRunning(ctx, sv); err != nil {
			return store.Server{}, "", err
		}
	}

	boot := connWith(sv, sv.AdminUser, stored)
	if bu := strings.TrimSpace(r.BootstrapUser); bu != "" {
		boot = connWith(sv, bu, r.BootstrapPassword)
	}
	e, err := adminEngine(ctx, sv.Engine, boot)
	if err != nil {
		return store.Server{}, "", fmt.Errorf("%w. Indique las credenciales de un administrador (p. ej. %s) para configurar el usuario root", err, defaultSuperUser(sv.Engine))
	}
	err = e.EnsureAdminLogin(ctx, user, pw)
	e.Close()
	if err != nil {
		return store.Server{}, "", fmt.Errorf("no se pudo configurar el usuario root %q: %s", user, CleanErr(err))
	}

	prev := sv.AdminUser
	sv.AdminUser = user
	if err := s.verifyAdmin(ctx, sv, pw); err != nil {
		return store.Server{}, "", err
	}
	upd, err := s.Store.UpdateServer(sv, pw)
	s.logOp(sv.ID, "configure-root", sv.Name+"/"+user, err)
	if err != nil {
		return store.Server{}, "", err
	}

	warning := ""
	if r.DropPrevious && !sameUser && prev != "" {
		e2, err := NewEngine(sv.Engine, connWith(sv, user, pw))
		if err == nil {
			// El root anterior suele ser dueño de las bases que creó: primero se pasan al nuevo.
			if err = e2.ReassignOwnership(ctx, prev, user); err == nil {
				err = e2.DropLogin(ctx, prev)
			}
			e2.Close()
		}
		s.logOp(sv.ID, "drop-login", sv.Name+"/"+prev, err)
		if err != nil {
			warning = fmt.Sprintf("El usuario root quedó configurado, pero no se pudo eliminar el login anterior %q: %s", prev, CleanErr(err))
		}
	}
	return upd, warning, nil
}

func defaultSuperUser(engineName string) string {
	if engineName == validate.EngineMSSQL {
		return "sa"
	}
	return "postgres"
}
