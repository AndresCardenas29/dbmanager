// Package services orquesta servicios locales, importación, backups y restauraciones
// usando store + dockerx + engine.
package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/engine"
	"dbmanager/internal/engine/mssql"
	"dbmanager/internal/engine/postgres"
	"dbmanager/internal/store"
	"dbmanager/internal/validate"
)

// Progress es el payload de los eventos de progreso hacia el frontend.
type Progress struct {
	ID        int64
	RequestID string
	Percent   int
	Message   string
	Done      bool
	Error     bool
}

// Emitter envía un evento ("service:progress", "backup:progress", "restore:progress").
type Emitter func(event string, p Progress)

// Services agrupa dependencias.
type Services struct {
	Store  *store.Store
	Docker func() (*dockerx.Client, error)
	Emit   Emitter
}

func (s *Services) emit(event string, p Progress) {
	if s.Emit != nil {
		s.Emit(event, p)
	}
}

const (
	DefaultMSSQLImage    = "mcr.microsoft.com/mssql/server:2022-latest"
	DefaultPostgresImage = "postgres:17"
	mssqlPlatform        = "linux/amd64"
	mssqlBackupDir       = "/var/opt/mssql/backup"
)

// ConnFor devuelve los parámetros de conexión de un servidor registrado.
func (s *Services) ConnFor(sv store.Server) (engine.Conn, error) {
	pw, err := s.Store.ServerPassword(sv)
	if err != nil {
		return engine.Conn{}, err
	}
	return connWith(sv, sv.AdminUser, pw), nil
}

// ConnAs devuelve los parámetros de conexión del servidor registrado con otro usuario y contraseña.
func (s *Services) ConnAs(sv store.Server, user, password string) engine.Conn {
	return connWith(sv, user, password)
}

// connWith devuelve los parámetros de conexión del servidor con otras credenciales.
func connWith(sv store.Server, user, password string) engine.Conn {
	host := sv.Host
	if host == "" {
		host = "localhost"
	}
	c := engine.Conn{Host: host, Port: sv.Port, User: user, Password: password, TLS: sv.TLS, TrustCert: sv.TrustServerCert}
	if sv.Mode == "local" {
		// Certificado autofirmado del contenedor: siempre se acepta.
		c.TrustCert = true
		c.TLS = sv.Engine == validate.EngineMSSQL
	}
	return c
}

// NewEngine crea el engine adecuado para el servidor. El llamador debe cerrarlo.
func NewEngine(engineName string, c engine.Conn) (engine.Engine, error) {
	switch engineName {
	case validate.EngineMSSQL:
		return mssql.New(c)
	case validate.EnginePostgres:
		return postgres.New(c)
	}
	return nil, fmt.Errorf("motor no soportado: %q", engineName)
}

// Engine abre el engine de un servidor registrado.
func (s *Services) Engine(sv store.Server) (engine.Engine, error) {
	c, err := s.ConnFor(sv)
	if err != nil {
		return nil, err
	}
	return NewEngine(sv.Engine, c)
}

// EngineByID carga el servidor y abre su engine.
func (s *Services) EngineByID(id int64) (store.Server, engine.Engine, error) {
	sv, err := s.Store.GetServer(id)
	if err != nil {
		return store.Server{}, nil, err
	}
	if sv.Mode == "local" {
		if err := s.requireRunning(context.Background(), sv); err != nil {
			return sv, nil, err
		}
	}
	e, err := s.Engine(sv)
	return sv, e, err
}

// requireRunning verifica que el contenedor de un servicio local esté en ejecución.
func (s *Services) requireRunning(ctx context.Context, sv store.Server) error {
	cli, err := s.Docker()
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := cli.Inspect(cctx, sv.ContainerName)
	if dockerx.IsNotFound(err) {
		return fmt.Errorf("el contenedor %q no existe en Docker", sv.ContainerName)
	}
	if err != nil {
		return err
	}
	if !info.Running {
		return fmt.Errorf("el servicio %q está detenido; inícielo primero", sv.Name)
	}
	return nil
}

// Status es el resultado de TestConnection.
type Status struct {
	State     string
	Reachable bool
	Version   string
	Message   string
}

// TestConnection prueba la conexión de un servidor local o remoto.
func (s *Services) TestConnection(ctx context.Context, sv store.Server) Status {
	st := Status{State: "remote"}
	if sv.Mode == "local" {
		cli, err := s.Docker()
		if err != nil {
			return Status{State: "missing", Message: err.Error()}
		}
		info, err := cli.Inspect(ctx, sv.ContainerName)
		if dockerx.IsNotFound(err) {
			return Status{State: "missing", Message: fmt.Sprintf("El contenedor %q no existe en Docker.", sv.ContainerName)}
		}
		if err != nil {
			return Status{State: "missing", Message: "No se pudo consultar Docker: " + err.Error()}
		}
		if !info.Running {
			return Status{State: "stopped", Message: "El contenedor está detenido."}
		}
		st.State = "running"
	}
	e, err := s.Engine(sv)
	if err != nil {
		st.Message = err.Error()
		return st
	}
	defer e.Close()
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	v, err := e.Ping(cctx)
	if err != nil {
		st.Message = "No se pudo conectar: " + CleanErr(err)
		return st
	}
	st.Reachable, st.Version, st.Message = true, v, "Conexión exitosa."
	return st
}

// CleanErr acorta mensajes de error de drivers.
func CleanErr(err error) string {
	if err == nil {
		return ""
	}
	m := strings.TrimSpace(WrapErr(err).Error())
	if len(m) > 600 {
		m = m[:600] + "..."
	}
	return m
}

// ---------- puertos ----------

// PickPort devuelve el primer puerto >= start que no esté en uso según isUsed (máx. 1000 intentos).
func PickPort(start int, isUsed func(int) bool) (int, error) {
	for p := start; p < start+1000 && p <= 65535; p++ {
		if !isUsed(p) {
			return p, nil
		}
	}
	return 0, errors.New("no se encontró un puerto libre")
}

// hostPortFree comprueba que el puerto pueda escucharse en el host.
func hostPortFree(p int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// publishedPorts devuelve los puertos del host publicados por contenedores Docker.
func (s *Services) publishedPorts(ctx context.Context) (map[int]bool, error) {
	cli, err := s.Docker()
	if err != nil {
		return nil, err
	}
	list, err := cli.ListContainers(ctx, true, "")
	if err != nil {
		return nil, err
	}
	used := map[int]bool{}
	for _, c := range list {
		for _, p := range c.Ports {
			used[p.HostPort] = true
		}
	}
	return used, nil
}

// ---------- utilidades ----------

var slugRe = regexp.MustCompile(`[^a-z0-9_.-]+`)

// Slug convierte un nombre en un identificador seguro para nombres de contenedor/volumen.
func Slug(name string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-._")
	if s == "" {
		s = "servicio"
	}
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}

// PostgresMountTarget devuelve el punto de montaje de datos según la versión de la imagen
// (a partir de la 18 la imagen oficial usa /var/lib/postgresql).
func PostgresMountTarget(image string) string {
	const newLayout, oldLayout = "/var/lib/postgresql", "/var/lib/postgresql/data"
	if strings.Contains(image, "@") {
		return newLayout // digest: versión desconocida, se asume el diseño actual
	}
	tag := ""
	if i := strings.LastIndex(image, ":"); i >= 0 && !strings.Contains(image[i:], "/") {
		tag = image[i+1:]
	}
	if tag == "" || tag[0] < '0' || tag[0] > '9' {
		return newLayout // latest, alpine, bookworm, trixie...
	}
	n := 0
	for _, r := range tag {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	if n >= 18 {
		return newLayout
	}
	return oldLayout
}

// MountTargetFromMajor devuelve el punto de montaje según PG_MAJOR de la imagen ("" si no se puede interpretar).
func MountTargetFromMajor(major string) string {
	n := 0
	for _, r := range strings.TrimSpace(major) {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	switch {
	case n == 0:
		return ""
	case n >= 18:
		return "/var/lib/postgresql"
	}
	return "/var/lib/postgresql/data"
}

func (s *Services) logOp(serverID int64, kind, target string, err error) {
	op := store.Operation{ServerID: serverID, Kind: kind, Target: target, Status: "ok"}
	if err != nil {
		op.Status, op.Message = "error", CleanErr(err)
	}
	s.Store.LogOperation(op)
}
