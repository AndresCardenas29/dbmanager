package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/services"
	"dbmanager/internal/sftp"
	"dbmanager/internal/store"
	"dbmanager/internal/validate"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	settingLastFileDir = "lastFileDir"
	settingLastDir     = "lastDirectory"
)

// App contiene los métodos expuestos al frontend.
type App struct {
	ctx     context.Context
	store   *store.Store
	initErr error

	dockerMu sync.Mutex
	docker   *dockerx.Client

	emit func(event string, data any) // envío de eventos al frontend (inyectable en pruebas)

	svcOnce sync.Once

	sftpOnce sync.Once
	sftpMgr  *sftp.Manager
	svc      *services.Services
}

// NewApp crea la estructura App.
func NewApp() *App {
	a := &App{}
	a.emit = func(event string, data any) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, event, data)
		}
	}
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.store, a.initErr = store.Open()
}

func (a *App) shutdown(ctx context.Context) {
	if a.store != nil {
		a.store.Close()
	}
	a.dockerMu.Lock()
	if a.docker != nil {
		a.docker.Close()
	}
	a.dockerMu.Unlock()
}

func (a *App) db() (*store.Store, error) {
	if a.initErr != nil {
		return nil, fmt.Errorf("no se pudo abrir el almacenamiento interno: %w", a.initErr)
	}
	if a.store == nil {
		return nil, errors.New("el almacenamiento interno aún no está listo")
	}
	return a.store, nil
}

func (a *App) dockerClient() (*dockerx.Client, error) {
	a.dockerMu.Lock()
	defer a.dockerMu.Unlock()
	if a.docker != nil {
		return a.docker, nil
	}
	c, err := dockerx.New()
	if err != nil {
		return nil, fmt.Errorf("no se pudo crear el cliente de Docker: %w", err)
	}
	a.docker = c
	return c, nil
}

// ---------- conversiones ----------

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func toServer(s store.Server) Server {
	return Server{
		ID: s.ID, Name: s.Name, Engine: s.Engine, Mode: s.Mode, Host: s.Host, Port: s.Port,
		AdminUser: s.AdminUser, HasPassword: s.AdminPasswordEnc != "", ContainerName: s.ContainerName,
		Image: s.Image, DataMode: s.DataMode, DataPath: s.DataPath, TLS: s.TLS,
		TrustServerCert: s.TrustServerCert, CreatedAt: fmtTime(s.CreatedAt),
	}
}

func (a *App) logOp(serverID int64, kind, target string, err error) {
	st, e := a.db()
	if e != nil {
		return
	}
	op := store.Operation{ServerID: serverID, Kind: kind, Target: target, Status: "ok"}
	if err != nil {
		op.Status, op.Message = "error", err.Error()
	}
	st.LogOperation(op)
}

func friendlyDBErr(err error) error {
	if err == nil {
		return nil
	}
	err = services.WrapErr(err)
	if errors.Is(err, store.ErrNotFound) {
		return errors.New("el servidor no existe")
	}
	if strings.Contains(err.Error(), "UNIQUE") {
		return errors.New("ya existe un servidor con ese nombre")
	}
	return err
}

// ---------- Entorno ----------

func (a *App) CheckEnvironment() (EnvStatus, error) {
	env := EnvStatus{OS: goruntime.GOOS, Arch: goruntime.GOARCH, Messages: []string{}}
	if goruntime.GOOS == "darwin" && goruntime.GOARCH == "arm64" {
		env.RosettaWarning = true
		env.Messages = append(env.Messages, "Mac con Apple Silicon: SQL Server se ejecuta como imagen linux/amd64; active Rosetta en Docker Desktop (Ajustes > General > Use Rosetta).")
	}
	cli, err := a.dockerClient()
	if err != nil {
		env.Messages = append(env.Messages, err.Error())
		return env, nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
	defer cancel()
	v, err := cli.Version(ctx)
	if err != nil {
		msg := "Docker no está instalado o no está en ejecución. Instale e inicie Docker Desktop (o el servicio docker)."
		if strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			msg = "Su usuario no tiene permiso sobre el socket de Docker. Agréguelo al grupo docker (sudo usermod -aG docker $USER) y vuelva a iniciar sesión."
		}
		env.Messages = append(env.Messages, msg)
		return env, nil
	}
	env.DockerAvailable = true
	env.DockerVersion = v.Version
	return env, nil
}

// ---------- Servidores ----------

func (a *App) ListServers() ([]Server, error) {
	st, err := a.db()
	if err != nil {
		return nil, err
	}
	list, err := st.ListServers()
	if err != nil {
		return nil, err
	}
	out := make([]Server, 0, len(list))
	for _, s := range list {
		out = append(out, toServer(s))
	}
	return out, nil
}

func (a *App) GetServer(id int64) (Server, error) {
	st, err := a.db()
	if err != nil {
		return Server{}, err
	}
	s, err := st.GetServer(id)
	if err != nil {
		return Server{}, friendlyDBErr(err)
	}
	return toServer(s), nil
}

func (a *App) AddRemoteServer(req RemoteServerRequest) (Server, error) {
	st, err := a.db()
	if err != nil {
		return Server{}, err
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Host = strings.TrimSpace(req.Host)
	req.AdminUser = strings.TrimSpace(req.AdminUser)
	if req.Name == "" {
		return Server{}, errors.New("el nombre es obligatorio")
	}
	if req.Engine != validate.EngineMSSQL && req.Engine != validate.EnginePostgres {
		return Server{}, errors.New("motor inválido: use mssql o postgres")
	}
	if req.Host == "" {
		return Server{}, errors.New("el host es obligatorio")
	}
	if req.Port < 1 || req.Port > 65535 {
		return Server{}, errors.New("el puerto debe estar entre 1 y 65535")
	}
	if req.AdminUser == "" {
		return Server{}, errors.New("el usuario administrador es obligatorio")
	}
	sv := store.Server{Name: req.Name, Engine: req.Engine, Mode: "remote", Host: req.Host, Port: req.Port,
		AdminUser: req.AdminUser, TLS: req.TLS, TrustServerCert: req.TrustServerCert}
	if req.Engine == validate.EnginePostgres {
		sv.PgDefaultDB = "postgres"
	}
	created, err := st.CreateServer(sv, req.AdminPassword)
	a.logOp(created.ID, "add-remote", req.Name, err)
	if err != nil {
		return Server{}, friendlyDBErr(err)
	}
	return toServer(created), nil
}

func (a *App) UpdateServer(id int64, req UpdateServerRequest) (Server, error) {
	st, err := a.db()
	if err != nil {
		return Server{}, err
	}
	cur, err := st.GetServer(id)
	if err != nil {
		return Server{}, friendlyDBErr(err)
	}
	if n := strings.TrimSpace(req.Name); n != "" {
		cur.Name = n
	} else {
		return Server{}, errors.New("el nombre es obligatorio")
	}
	if cur.Mode == "remote" {
		if h := strings.TrimSpace(req.Host); h != "" {
			cur.Host = h
		} else {
			return Server{}, errors.New("el host es obligatorio")
		}
		if req.Port < 1 || req.Port > 65535 {
			return Server{}, errors.New("el puerto debe estar entre 1 y 65535")
		}
		cur.Port = req.Port
		cur.TLS = req.TLS
		cur.TrustServerCert = req.TrustServerCert
	}
	if u := strings.TrimSpace(req.AdminUser); u != "" {
		cur.AdminUser = u
	}
	upd, err := st.UpdateServer(cur, req.AdminPassword)
	a.logOp(id, "update-server", cur.Name, err)
	if err != nil {
		return Server{}, friendlyDBErr(err)
	}
	return toServer(upd), nil
}

func (a *App) DeleteServer(id int64, removeContainer bool, removeData bool) error {
	st, err := a.db()
	if err != nil {
		return err
	}
	sv, err := st.GetServer(id)
	if err != nil {
		return friendlyDBErr(err)
	}
	if sv.Mode == "local" && removeContainer && sv.ContainerName != "" {
		cli, err := a.dockerClient()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
		defer cancel()
		if err := cli.Remove(ctx, sv.ContainerName, false); err != nil && !dockerx.IsNotFound(err) {
			e := fmt.Errorf("no se pudo eliminar el contenedor: %w", err)
			a.logOp(id, "delete-server", sv.Name, e)
			return e
		}
		if removeData && sv.DataMode == "volume" && sv.DataPath != "" {
			if err := cli.RemoveVolume(ctx, sv.DataPath); err != nil && !dockerx.IsNotFound(err) {
				e := fmt.Errorf("el contenedor se eliminó pero no se pudo borrar el volumen: %w", err)
				a.logOp(id, "delete-server", sv.Name, e)
				return e
			}
		}
		// Las carpetas del host (bind) nunca se borran automáticamente.
	}
	err = st.DeleteServer(id)
	a.logOp(id, "delete-server", sv.Name, err)
	return friendlyDBErr(err)
}

// localContainer devuelve el servidor y el cliente Docker para operaciones sobre contenedores.
func (a *App) localContainer(id int64) (store.Server, *dockerx.Client, error) {
	st, err := a.db()
	if err != nil {
		return store.Server{}, nil, err
	}
	sv, err := st.GetServer(id)
	if err != nil {
		return store.Server{}, nil, friendlyDBErr(err)
	}
	if sv.Mode != "local" || sv.ContainerName == "" {
		return sv, nil, errors.New("esta operación solo aplica a servicios locales en Docker")
	}
	cli, err := a.dockerClient()
	return sv, cli, err
}

func (a *App) containerAction(id int64, kind string, fn func(context.Context, *dockerx.Client, string) error) error {
	sv, cli, err := a.localContainer(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	err = fn(ctx, cli, sv.ContainerName)
	if dockerx.IsNotFound(err) {
		err = fmt.Errorf("el contenedor %q no existe en Docker", sv.ContainerName)
	}
	a.logOp(id, kind, sv.Name, err)
	return err
}

func (a *App) StartServer(id int64) error {
	return a.containerAction(id, "start", func(ctx context.Context, c *dockerx.Client, n string) error { return c.Start(ctx, n) })
}

func (a *App) StopServer(id int64) error {
	return a.containerAction(id, "stop", func(ctx context.Context, c *dockerx.Client, n string) error { return c.Stop(ctx, n) })
}

func (a *App) RestartServer(id int64) error {
	return a.containerAction(id, "restart", func(ctx context.Context, c *dockerx.Client, n string) error { return c.Restart(ctx, n) })
}

func (a *App) ServerStatus(id int64) (ServerStatus, error) {
	st, err := a.db()
	if err != nil {
		return ServerStatus{}, err
	}
	sv, err := st.GetServer(id)
	if err != nil {
		return ServerStatus{}, friendlyDBErr(err)
	}
	if sv.Mode == "remote" {
		return ServerStatus{State: "remote", Message: "Servidor remoto: la comprobación de conexión usa TestConnection."}, nil
	}
	cli, err := a.dockerClient()
	if err != nil {
		return ServerStatus{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	info, err := cli.Inspect(ctx, sv.ContainerName)
	if dockerx.IsNotFound(err) {
		return ServerStatus{State: "missing", Message: fmt.Sprintf("El contenedor %q no existe en Docker.", sv.ContainerName)}, nil
	}
	if err != nil {
		return ServerStatus{State: "missing", Message: "No se pudo consultar Docker: " + err.Error()}, nil
	}
	if info.Running {
		return ServerStatus{State: "running", Message: info.Status}, nil
	}
	return ServerStatus{State: "stopped", Message: info.Status}, nil
}

func (a *App) ServerLogs(id int64, tail int) (string, error) {
	sv, cli, err := a.localContainer(id)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	out, err := cli.Logs(ctx, sv.ContainerName, tail)
	if dockerx.IsNotFound(err) {
		return "", fmt.Errorf("el contenedor %q no existe en Docker", sv.ContainerName)
	}
	return out, err
}

// ---------- Bases de datos ----------

// ---------- Backups / restauraciones ----------

func (a *App) pickerDir(setting string) string {
	st, err := a.db()
	if err != nil {
		return ""
	}
	v, _ := st.GetSetting(setting)
	return v
}

func (a *App) remember(setting, dir string) {
	if st, err := a.db(); err == nil && dir != "" {
		st.SetSetting(setting, dir)
	}
}

// PickFile abre el diálogo nativo de archivo y recuerda la última carpeta. Devuelve "" si se cancela.
func (a *App) PickFile(title string, filters []FileFilter) (string, error) {
	opts := wruntime.OpenDialogOptions{Title: title, DefaultDirectory: a.pickerDir(settingLastFileDir)}
	for _, f := range filters {
		opts.Filters = append(opts.Filters, wruntime.FileFilter{DisplayName: f.DisplayName, Pattern: f.Pattern})
	}
	p, err := wruntime.OpenFileDialog(a.ctx, opts)
	if err != nil {
		return "", err
	}
	if p != "" {
		a.remember(settingLastFileDir, filepath.Dir(p))
	}
	return p, nil
}

// PickDirectory abre el diálogo nativo de carpeta y recuerda la última. Devuelve "" si se cancela.
func (a *App) PickDirectory(title string) (string, error) {
	p, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: title, DefaultDirectory: a.pickerDir(settingLastDir), CanCreateDirectories: true,
	})
	if err != nil {
		return "", err
	}
	if p != "" {
		a.remember(settingLastDir, p)
	}
	return p, nil
}

func (a *App) ListBackups(serverId int64) ([]BackupRecord, error) {
	st, err := a.db()
	if err != nil {
		return nil, err
	}
	list, err := st.ListBackups(serverId)
	if err != nil {
		return nil, err
	}
	out := make([]BackupRecord, 0, len(list))
	for _, b := range list {
		out = append(out, BackupRecord{ID: b.ID, ServerID: b.ServerID, ServerName: b.ServerName, Database: b.Database,
			FilePath: b.FilePath, SizeBytes: b.SizeBytes, CreatedAt: fmtTime(b.CreatedAt), Status: b.Status, Message: b.Message})
	}
	return out, nil
}

func (a *App) ListRestores(serverId int64) ([]RestoreRecord, error) {
	st, err := a.db()
	if err != nil {
		return nil, err
	}
	list, err := st.ListRestores(serverId)
	if err != nil {
		return nil, err
	}
	out := make([]RestoreRecord, 0, len(list))
	for _, x := range list {
		out = append(out, RestoreRecord{ID: x.ID, ServerID: x.ServerID, ServerName: x.ServerName, SourceFile: x.SourceFile,
			TargetDatabase: x.TargetDatabase, DataDir: x.DataDir, LogDir: x.LogDir, Status: x.Status, Message: x.Message,
			StartedAt: fmtTime(x.StartedAt), FinishedAt: fmtTime(x.FinishedAt)})
	}
	return out, nil
}

// ---------- Ajustes e historial ----------

func (a *App) GetSettings() (map[string]string, error) {
	st, err := a.db()
	if err != nil {
		return nil, err
	}
	all, err := st.GetSettings()
	if err != nil {
		return nil, err
	}
	for k := range all {
		if isSecretSetting(k) {
			delete(all, k) // los secretos cifrados no salen hacia el frontend
		}
	}
	return all, nil
}

func isSecretSetting(key string) bool { return strings.HasSuffix(key, "Enc") }

func (a *App) SetSetting(key string, value string) error {
	st, err := a.db()
	if err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("la clave del ajuste es obligatoria")
	}
	if isSecretSetting(key) {
		return errors.New("este ajuste solo se puede cambiar desde su sección de Ajustes")
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "mcp") {
		return errors.New("los ajustes de MCP solo se cambian desde Andres Tools > MCP")
	}
	return st.SetSetting(key, value)
}

func (a *App) ListOperations(limit int) ([]Operation, error) {
	st, err := a.db()
	if err != nil {
		return nil, err
	}
	list, err := st.ListOperations(limit)
	if err != nil {
		return nil, err
	}
	out := make([]Operation, 0, len(list))
	for _, o := range list {
		out = append(out, Operation{ID: o.ID, ServerID: o.ServerID, Kind: o.Kind, Target: o.Target, Status: o.Status,
			Message: o.Message, CreatedAt: fmtTime(o.CreatedAt)})
	}
	return out, nil
}
