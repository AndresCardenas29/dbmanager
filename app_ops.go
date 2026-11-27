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
	"dbmanager/internal/validate"
)

// services construye (una vez) la capa de orquestación.
func (a *App) services() (*services.Services, error) {
	st, err := a.db()
	if err != nil {
		return nil, err
	}
	a.svcOnce.Do(func() {
		a.svc = &services.Services{Store: st, Docker: a.dockerClient, Emit: func(event string, p services.Progress) {
			if a.emit != nil {
				a.emit(event, ProgressEvent{ID: p.ID, RequestID: p.RequestID, Percent: p.Percent,
					Message: p.Message, Done: p.Done, Error: p.Error})
			}
		}}
	})
	return a.svc, nil
}

// ProgressEventType no hace nada; existe para que Wails exporte el tipo ProgressEvent al frontend.
func (a *App) ProgressEventType() ProgressEvent { return ProgressEvent{} }

// withEngine abre el engine del servidor y ejecuta fn con un contexto con plazo.
func (a *App) withEngine(serverID int64, timeout time.Duration, fn func(ctx context.Context, sv store.Server, e engine.Engine) error) error {
	svc, err := a.services()
	if err != nil {
		return err
	}
	sv, e, err := svc.EngineByID(serverID)
	if err != nil {
		return friendlyDBErr(err)
	}
	defer e.Close()
	ctx, cancel := context.WithTimeout(a.ctx, timeout)
	defer cancel()
	return friendlyDBErr(fn(ctx, sv, e))
}

// opErr registra el resultado de una operación de escritura y devuelve el error.
func (a *App) opErr(serverID int64, kind, target string, err error) error {
	a.logOp(serverID, kind, target, err)
	return err
}

func toServerStatus(s services.Status) ServerStatus {
	return ServerStatus{State: s.State, Reachable: s.Reachable, Version: s.Version, Message: s.Message}
}

// ---------- Servicios locales ----------

func (a *App) CreateLocalService(req CreateServiceRequest) (Server, error) {
	svc, err := a.services()
	if err != nil {
		return Server{}, err
	}
	rootUser, rootPw, err := a.store.RootDefaults()
	if err != nil {
		return Server{}, err
	}
	if u := strings.TrimSpace(req.RootUser); u != "" {
		rootUser = u
	}
	if req.RootPassword != "" {
		rootPw = req.RootPassword
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Minute)
	defer cancel()
	sv, err := svc.CreateLocal(ctx, services.CreateRequest{RequestID: req.RequestID, Name: req.Name, Engine: req.Engine,
		Image: req.Image, Port: req.Port, AdminPassword: req.AdminPassword, DataMode: req.DataMode, DataPath: req.DataPath,
		MemoryMB: req.MemoryMB, Edition: req.Edition, ExposeOnNetwork: req.ExposeOnNetwork,
		RootUser: rootUser, RootPassword: rootPw})
	if err != nil {
		a.logOp(0, "create-service", req.Name, err)
		return Server{}, friendlyDBErr(err)
	}
	return toServer(sv), nil
}

func (a *App) ListImportableContainers() ([]ContainerInfo, error) {
	svc, err := a.services()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	list, err := svc.ListImportable(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ContainerInfo, 0, len(list))
	for _, c := range list {
		out = append(out, ContainerInfo{ID: c.ID, Name: c.Name, Image: c.Image, Engine: c.Engine, State: c.State, Status: c.Status,
			HostPort: c.HostPort, ContainerPort: c.ContainerPort, DataMode: c.DataMode, DataPath: c.DataPath})
	}
	return out, nil
}

func (a *App) ImportContainer(req ImportContainerRequest) (Server, error) {
	svc, err := a.services()
	if err != nil {
		return Server{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
	defer cancel()
	sv, err := svc.Import(ctx, services.ImportRequest{ContainerName: req.ContainerName, AdminUser: req.AdminUser, AdminPassword: req.AdminPassword})
	if err != nil {
		return Server{}, friendlyDBErr(err)
	}
	return toServer(sv), nil
}

func (a *App) TestConnection(id int64) (ServerStatus, error) {
	svc, err := a.services()
	if err != nil {
		return ServerStatus{}, err
	}
	sv, err := a.store.GetServer(id)
	if err != nil {
		return ServerStatus{}, friendlyDBErr(err)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	return toServerStatus(svc.TestConnection(ctx, sv)), nil
}

// TestRemoteConnection prueba una conexión sin guardar nada ni registrar operaciones.
func (a *App) TestRemoteConnection(req RemoteServerRequest) (ServerStatus, error) {
	if req.Engine != validate.EngineMSSQL && req.Engine != validate.EnginePostgres {
		return ServerStatus{}, errors.New("motor inválido: use mssql o postgres")
	}
	if strings.TrimSpace(req.Host) == "" {
		return ServerStatus{}, errors.New("el host es obligatorio")
	}
	if req.Port < 1 || req.Port > 65535 {
		return ServerStatus{}, errors.New("el puerto debe estar entre 1 y 65535")
	}
	e, err := services.NewEngine(req.Engine, engine.Conn{Host: strings.TrimSpace(req.Host), Port: req.Port,
		User: strings.TrimSpace(req.AdminUser), Password: req.AdminPassword, TLS: req.TLS, TrustCert: req.TrustServerCert})
	if err != nil {
		return ServerStatus{}, err
	}
	defer e.Close()
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	v, err := e.Ping(ctx)
	if err != nil {
		return ServerStatus{State: "remote", Message: "No se pudo conectar: " + services.CleanErr(err)}, nil
	}
	return ServerStatus{State: "remote", Reachable: true, Version: v, Message: "Conexión exitosa."}, nil
}

// ---------- Usuario root ----------

// GetRootDefaults devuelve el usuario root que se crea en los servicios nuevos (sin la contraseña).
func (a *App) GetRootDefaults() (RootDefaults, error) {
	st, err := a.db()
	if err != nil {
		return RootDefaults{}, err
	}
	u, pw, err := st.RootDefaults()
	if err != nil {
		return RootDefaults{}, err
	}
	return RootDefaults{User: u, PasswordIsDefault: pw == store.DefaultRootPassword}, nil
}

// SetRootDefaults guarda el usuario root predeterminado; password vacío = conservar la actual.
func (a *App) SetRootDefaults(user string, password string) error {
	st, err := a.db()
	if err != nil {
		return err
	}
	user = strings.TrimSpace(user)
	for _, eng := range []string{validate.EngineMSSQL, validate.EnginePostgres} {
		if err := services.ValidateRootUser(eng, user); err != nil {
			return err
		}
	}
	err = st.SetRootDefaults(user, password)
	a.logOp(0, "set-root-defaults", user, err)
	return err
}

// GetRootStatus indica con qué usuario se administra el servidor y si es administrador total.
func (a *App) GetRootStatus(serverId int64) (RootStatus, error) {
	svc, err := a.services()
	if err != nil {
		return RootStatus{}, err
	}
	sv, err := a.store.GetServer(serverId)
	if err != nil {
		return RootStatus{}, friendlyDBErr(err)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 40*time.Second)
	defer cancel()
	r := svc.RootStatus(ctx, sv)
	return RootStatus{User: r.User, Reachable: r.Reachable, IsAdmin: r.IsAdmin, Message: r.Message}, nil
}

// ConfigureRoot crea o cambia el usuario root del servidor (en el motor y en DB Manager).
func (a *App) ConfigureRoot(serverId int64, req ConfigureRootRequest) (ConfigureRootResult, error) {
	svc, err := a.services()
	if err != nil {
		return ConfigureRootResult{}, err
	}
	sv, err := a.store.GetServer(serverId)
	if err != nil {
		return ConfigureRootResult{}, friendlyDBErr(err)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	upd, warning, err := svc.ConfigureRoot(ctx, sv, services.ConfigureRootRequest{User: req.User, Password: req.Password,
		BootstrapUser: req.BootstrapUser, BootstrapPassword: req.BootstrapPassword, DropPrevious: req.DropPrevious})
	if err != nil {
		a.logOp(serverId, "configure-root", sv.Name, err)
		return ConfigureRootResult{}, friendlyDBErr(err)
	}
	return ConfigureRootResult{Server: toServer(upd), Warning: warning}, nil
}

// ---------- Bases de datos ----------

func (a *App) ListDatabases(serverId int64) ([]Database, error) {
	out := []Database{}
	err := a.withEngine(serverId, 60*time.Second, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		list, err := e.ListDatabases(ctx)
		if err != nil {
			return err
		}
		for _, d := range list {
			out = append(out, Database{Name: d.Name, SizeMB: d.SizeMB, Owner: d.Owner, State: strings.ToUpper(d.State), System: d.System})
		}
		return nil
	})
	return out, err
}

func (a *App) CreateDatabase(serverId int64, name string, owner string) error {
	return a.withEngine(serverId, 2*time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		return a.opErr(serverId, "create-database", sv.Name+"/"+name, e.CreateDatabase(ctx, strings.TrimSpace(name), strings.TrimSpace(owner)))
	})
}

func (a *App) DropDatabase(serverId int64, name string) error {
	return a.withEngine(serverId, 5*time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		return a.opErr(serverId, "drop-database", sv.Name+"/"+name, e.DropDatabase(ctx, name))
	})
}

// ---------- Usuarios y permisos ----------

func (a *App) ListLogins(serverId int64) ([]Login, error) {
	out := []Login{}
	err := a.withEngine(serverId, 60*time.Second, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		list, err := e.ListLogins(ctx)
		if err != nil {
			return err
		}
		for _, l := range list {
			rs := l.ServerRoles
			if rs == nil {
				rs = []string{}
			}
			out = append(out, Login{Name: l.Name, ServerRoles: rs, Disabled: l.Disabled, IsSystem: l.IsSystem})
		}
		return nil
	})
	return out, err
}

func (a *App) CreateLogin(serverId int64, req CreateLoginRequest) error {
	return a.withEngine(serverId, time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		name := strings.TrimSpace(req.Name)
		err := a.opErr(serverId, "create-login", sv.Name+"/"+req.Name, e.CreateLogin(ctx, name, req.Password, req.ServerRoles))
		if err == nil {
			a.saveLoginSecret(serverId, name, req.Password)
		}
		return err
	})
}

func (a *App) DropLogin(serverId int64, name string) error {
	return a.withEngine(serverId, time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		err := a.opErr(serverId, "drop-login", sv.Name+"/"+name, e.DropLogin(ctx, name))
		if err == nil && a.store != nil {
			a.store.DeleteLoginSecret(serverId, name) // mejor esfuerzo
		}
		return err
	})
}

func (a *App) ChangePassword(serverId int64, name string, password string) error {
	return a.withEngine(serverId, time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		err := a.opErr(serverId, "change-password", sv.Name+"/"+name, e.ChangePassword(ctx, name, password))
		if err == nil {
			a.saveLoginSecret(serverId, name, password)
		}
		return err
	})
}

func (a *App) SetServerRoles(serverId int64, name string, roles []string) error {
	return a.withEngine(serverId, time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		return a.opErr(serverId, "set-server-roles", sv.Name+"/"+name, e.SetServerRoles(ctx, name, roles))
	})
}

func (a *App) GetPermissionMatrix(serverId int64) (PermissionMatrix, error) {
	out := PermissionMatrix{Logins: []string{}, Databases: []string{}, Cells: []PermissionCell{}}
	err := a.withEngine(serverId, 2*time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		m, err := e.GetPermissionMatrix(ctx)
		if err != nil {
			return err
		}
		out.Logins, out.Databases = m.Logins, m.Databases
		for _, c := range m.Cells {
			out.Cells = append(out.Cells, PermissionCell{Login: c.Login, Database: c.Database, Level: c.Level})
		}
		return nil
	})
	return out, err
}

// CreateDatabaseUser crea un login sin roles de servidor con acceso solo a una base de datos.
// Así cada base tiene sus propios usuarios y el root es el único con acceso a todas.
func (a *App) CreateDatabaseUser(serverId int64, database string, req CreateLoginRequest, level string) error {
	if level == "" || level == "none" {
		return errors.New("elija el nivel de acceso del usuario en la base de datos")
	}
	return a.withEngine(serverId, 2*time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		name := strings.TrimSpace(req.Name)
		target := fmt.Sprintf("%s/%s/%s=%s", sv.Name, database, name, level)
		if ok, err := e.DatabaseExists(ctx, database); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("la base de datos %q no existe", database)
		}
		if err := e.CreateLogin(ctx, name, req.Password, nil); err != nil {
			return a.opErr(serverId, "create-db-user", target, err)
		}
		if err := e.SetDatabaseAccess(ctx, name, database, level); err != nil {
			e.DropLogin(ctx, name) // deshace: no dejar un login suelto sin acceso
			return a.opErr(serverId, "create-db-user", target, fmt.Errorf("no se pudo dar acceso a la base de datos: %w", err))
		}
		a.saveLoginSecret(serverId, name, req.Password)
		return a.opErr(serverId, "create-db-user", target, nil)
	})
}

func (a *App) SetDatabaseAccess(serverId int64, login string, database string, level string) error {
	return a.withEngine(serverId, 2*time.Minute, func(ctx context.Context, sv store.Server, e engine.Engine) error {
		return a.opErr(serverId, "set-access", fmt.Sprintf("%s/%s/%s=%s", sv.Name, database, login, level),
			e.SetDatabaseAccess(ctx, login, database, level))
	})
}

// ---------- Backups / restauraciones ----------

func toBackupRecord(b store.Backup) BackupRecord {
	return BackupRecord{ID: b.ID, ServerID: b.ServerID, ServerName: b.ServerName, Database: b.Database, FilePath: b.FilePath,
		SizeBytes: b.SizeBytes, CreatedAt: fmtTime(b.CreatedAt), Status: b.Status, Message: b.Message}
}

func toRestoreRecord(x store.Restore) RestoreRecord {
	return RestoreRecord{ID: x.ID, ServerID: x.ServerID, ServerName: x.ServerName, SourceFile: x.SourceFile,
		TargetDatabase: x.TargetDatabase, DataDir: x.DataDir, LogDir: x.LogDir, Status: x.Status, Message: x.Message,
		StartedAt: fmtTime(x.StartedAt), FinishedAt: fmtTime(x.FinishedAt)}
}

// CreateBackup es síncrono: devuelve al terminar. Los eventos "backup:progress" llevan el id del registro
// y el requestId recibido.
func (a *App) CreateBackup(req BackupRequest) (BackupRecord, error) {
	svc, err := a.services()
	if err != nil {
		return BackupRecord{}, err
	}
	sv, err := a.store.GetServer(req.ServerID)
	if err != nil {
		return BackupRecord{}, friendlyDBErr(err)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 6*time.Hour)
	defer cancel()
	rec, err := svc.Backup(ctx, sv, req.Database, req.DestDir, req.RequestID)
	if err != nil {
		return BackupRecord{}, friendlyDBErr(err)
	}
	return toBackupRecord(rec), nil
}

func (a *App) InspectBackup(serverId int64, filePath string) (BackupInfo, error) {
	svc, err := a.services()
	if err != nil {
		return BackupInfo{}, err
	}
	sv, err := a.store.GetServer(serverId)
	if err != nil {
		return BackupInfo{}, friendlyDBErr(err)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	defer cancel()
	info, err := svc.Inspect(ctx, sv, filePath)
	if err != nil {
		return BackupInfo{}, friendlyDBErr(err)
	}
	out := BackupInfo{Format: info.Format, DatabaseName: info.DatabaseName, Files: []BackupFile{}}
	for _, f := range info.Files {
		out.Files = append(out.Files, BackupFile{LogicalName: f.LogicalName, PhysicalName: f.PhysicalName, Type: f.Type})
	}
	return out, nil
}

// RestoreBackup es síncrono; los eventos "restore:progress" llevan el id del registro y el requestId.
func (a *App) RestoreBackup(req RestoreRequest) (RestoreRecord, error) {
	svc, err := a.services()
	if err != nil {
		return RestoreRecord{}, err
	}
	sv, err := a.store.GetServer(req.ServerID)
	if err != nil {
		return RestoreRecord{}, friendlyDBErr(err)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 6*time.Hour)
	defer cancel()
	rec, err := svc.Restore(ctx, sv, services.RestoreRequest{RequestID: req.RequestID, FilePath: req.FilePath,
		TargetDatabase: req.TargetDatabase, DataDir: req.DataDir, LogDir: req.LogDir, Replace: req.Replace})
	if err != nil {
		return RestoreRecord{}, friendlyDBErr(err)
	}
	return toRestoreRecord(rec), nil
}
