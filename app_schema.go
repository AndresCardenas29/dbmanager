package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dbmanager/internal/engine"
	"dbmanager/internal/services"
	"dbmanager/internal/store"
)

// saveLoginSecret guarda la contraseña del login (mejor esfuerzo: nunca hace fallar la operación).
func (a *App) saveLoginSecret(serverID int64, login, password string) {
	if a.store == nil || login == "" || password == "" {
		return
	}
	a.store.SetLoginSecret(serverID, login, password)
}

func (a *App) ListSchemas(serverId int64, database string) ([]SchemaInfo, error) {
	out := []SchemaInfo{}
	err := a.withEngine(serverId, time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		list, err := e.ListSchemas(ctx, database)
		if err != nil {
			return err
		}
		for _, s := range list {
			t := s.Tables
			if t == nil {
				t = []string{}
			}
			out = append(out, SchemaInfo{Name: s.Name, Owner: s.Owner, Tables: t, System: s.System})
		}
		return nil
	})
	return out, err
}

func (a *App) CreateSchema(serverId int64, database string, name string) error {
	return a.withEngine(serverId, time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		name = strings.TrimSpace(name)
		return a.opErr(serverId, "create-schema", fmt.Sprintf("%s/%s/%s", sv.Name, database, name), e.CreateSchema(ctx, database, name))
	})
}

func (a *App) ListObjectGrants(serverId int64, database string) ([]ObjectGrant, error) {
	out := []ObjectGrant{}
	err := a.withEngine(serverId, time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		list, err := e.ListObjectGrants(ctx, database)
		if err != nil {
			return err
		}
		for _, g := range list {
			p := g.Privileges
			if p == nil {
				p = []string{}
			}
			out = append(out, ObjectGrant{Login: g.Login, Schema: g.Schema, Table: g.Table, Privileges: p})
		}
		return nil
	})
	return out, err
}

func (a *App) SetObjectGrant(serverId int64, database string, req ObjectGrantRequest) error {
	return a.withEngine(serverId, 2*time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		target := fmt.Sprintf("%s/%s/%s: %s.%s=%s", sv.Name, database, req.Login, req.Schema, req.Table, strings.Join(req.Privileges, ","))
		return a.opErr(serverId, "set-grant", target,
			e.SetObjectGrant(ctx, database, strings.TrimSpace(req.Login), strings.TrimSpace(req.Schema), strings.TrimSpace(req.Table), req.Privileges))
	})
}

// ListConnectionUsers lista los usuarios con los que se puede armar una cadena de conexión: primero el root del
// servidor y luego los logins con acceso a la base. Con database vacía devuelve solo el root.
func (a *App) ListConnectionUsers(serverId int64, database string) ([]ConnectionUser, error) {
	st, err := a.db()
	if err != nil {
		return nil, err
	}
	sv, err := st.GetServer(serverId)
	if err != nil {
		return nil, friendlyDBErr(err)
	}
	out := []ConnectionUser{{Login: sv.AdminUser, Level: engine.LevelOwner, IsRoot: true, HasPassword: sv.AdminPasswordEnc != ""}}
	if database == "" {
		return out, nil
	}
	secrets, _ := st.ListLoginSecretLogins(serverId)
	m, err := a.GetPermissionMatrix(serverId)
	if err != nil {
		return nil, err
	}
	for _, c := range m.Cells {
		if c.Database != database || c.Level == engine.LevelNone || strings.EqualFold(c.Login, sv.AdminUser) {
			continue
		}
		out = append(out, ConnectionUser{Login: c.Login, Level: c.Level, HasPassword: secrets[strings.ToLower(c.Login)]})
	}
	return out, nil
}

// GetLoginPassword devuelve la contraseña guardada de un login (o la del root del servidor). Es la única
// excepción deliberada a "las contraseñas nunca salen al frontend": se usa para copiar cadenas de conexión.
func (a *App) GetLoginPassword(serverId int64, login string) (string, error) {
	st, err := a.db()
	if err != nil {
		return "", err
	}
	sv, err := st.GetServer(serverId)
	if err != nil {
		return "", friendlyDBErr(err)
	}
	if strings.EqualFold(login, sv.AdminUser) {
		pw, err := st.ServerPassword(sv)
		if err != nil {
			return "", errors.New("no se pudo leer la contraseña guardada del usuario root")
		}
		return pw, nil
	}
	pw, ok, err := st.LoginSecret(serverId, login)
	if err != nil {
		return "", errors.New("no se pudo leer la contraseña guardada del usuario")
	}
	if !ok {
		return "", nil
	}
	return pw, nil
}

// RememberLoginPassword guarda la contraseña de un login existente tras verificar que se puede conectar con ella.
func (a *App) RememberLoginPassword(serverId int64, login string, password string) error {
	login = strings.TrimSpace(login)
	if login == "" || password == "" {
		return errors.New("indique el usuario y la contraseña")
	}
	svc, err := a.services()
	if err != nil {
		return err
	}
	sv, err := a.store.GetServer(serverId)
	if err != nil {
		return friendlyDBErr(err)
	}
	e, err := services.NewEngine(sv.Engine, svc.ConnAs(sv, login, password))
	if err != nil {
		return friendlyDBErr(err)
	}
	defer e.Close()
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	if _, err := e.Ping(ctx); err != nil {
		return errors.New("no se pudo conectar con ese usuario y contraseña: " + services.CleanErr(err))
	}
	if err := a.store.SetLoginSecret(serverId, login, password); err != nil {
		return errors.New("no se pudo guardar la contraseña")
	}
	return nil
}
