package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/engine"
	"dbmanager/internal/store"
	"dbmanager/internal/validate"
)

var volumeNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// Importable es un contenedor existente que se puede registrar.
type Importable struct {
	ID            string
	Name          string
	Image         string
	Engine        string
	State         string
	Status        string
	HostPort      int
	ContainerPort int
	DataMode      string
	DataPath      string
}

// DetectEngine aplica las heurísticas de importación. Devuelve "" si no es un motor soportado.
func DetectEngine(image string, cmd, entrypoint, envNames []string, mounts []dockerx.MountInfo) string {
	img := strings.ToLower(image)
	has := func(names ...string) bool {
		for _, e := range envNames {
			for _, n := range names {
				if e == n {
					return true
				}
			}
		}
		return false
	}
	joined := strings.ToLower(strings.Join(append(append([]string{}, entrypoint...), cmd...), " "))
	if strings.Contains(img, "mssql/server") || strings.Contains(img, "azure-sql-edge") ||
		strings.Contains(joined, "sqlservr") || has("ACCEPT_EULA") {
		return validate.EngineMSSQL
	}
	base := img
	if i := strings.LastIndex(base, ":"); i >= 0 {
		base = base[:i]
	}
	if base == "postgres" || strings.HasSuffix(base, "/postgres") || strings.Contains(base, "postgis/postgis") ||
		strings.Contains(base, "bitnami/postgresql") || has("POSTGRES_PASSWORD", "POSTGRES_USER") {
		return validate.EnginePostgres
	}
	for _, m := range mounts {
		if m.Destination == "/var/lib/postgresql/data" {
			return validate.EnginePostgres
		}
	}
	return ""
}

func describe(info dockerx.ContainerInfo) (Importable, bool) {
	eng := DetectEngine(info.Image, info.Cmd, info.Entrypoint, info.EnvNames, info.Mounts)
	if eng == "" {
		return Importable{}, false
	}
	cport := 1433
	if eng == validate.EnginePostgres {
		cport = 5432
	}
	im := Importable{ID: info.ID, Name: info.Name, Image: info.Image, Engine: eng, State: info.State, Status: info.Status, ContainerPort: cport}
	for _, p := range info.Ports {
		if p.ContainerPort == cport && p.HostPort > 0 {
			im.HostPort = p.HostPort
			break
		}
	}
	if im.HostPort == 0 && len(info.Ports) > 0 {
		im.HostPort = info.Ports[0].HostPort
		im.ContainerPort = info.Ports[0].ContainerPort
	}
	for _, m := range info.Mounts {
		switch m.Destination {
		case "/var/opt/mssql/data", "/var/opt/mssql", "/var/lib/postgresql/data", "/var/lib/postgresql":
			if m.Type == "bind" {
				im.DataMode, im.DataPath = "bind", m.Source
			} else {
				im.DataMode, im.DataPath = "volume", m.Name
			}
		}
		if im.DataMode != "" {
			break
		}
	}
	return im, true
}

// ListImportable lista contenedores mssql/postgres no registrados.
func (s *Services) ListImportable(ctx context.Context) ([]Importable, error) {
	cli, err := s.Docker()
	if err != nil {
		return nil, err
	}
	servers, err := s.Store.ListServers()
	if err != nil {
		return nil, err
	}
	registered := map[string]bool{}
	for _, sv := range servers {
		if sv.ContainerName != "" {
			registered[sv.ContainerName] = true
		}
	}
	list, err := cli.ListContainers(ctx, true, "")
	if err != nil {
		return nil, err
	}
	out := []Importable{}
	for _, c := range list {
		if registered[c.Name] || c.Labels["dbmanager.temp"] == "true" {
			continue
		}
		info, err := cli.Inspect(ctx, c.ID)
		if err != nil {
			continue
		}
		if im, ok := describe(info); ok {
			out = append(out, im)
		}
	}
	return out, nil
}

// ImportRequest son los datos para importar un contenedor existente.
type ImportRequest struct {
	ContainerName string
	AdminUser     string
	AdminPassword string
}

// Import registra un contenedor existente (sin recrearlo) tras verificar la conexión.
func (s *Services) Import(ctx context.Context, r ImportRequest) (store.Server, error) {
	r.ContainerName = strings.TrimSpace(r.ContainerName)
	if r.ContainerName == "" {
		return store.Server{}, errors.New("indique el contenedor a importar")
	}
	cli, err := s.Docker()
	if err != nil {
		return store.Server{}, err
	}
	info, err := cli.Inspect(ctx, r.ContainerName)
	if dockerx.IsNotFound(err) {
		return store.Server{}, fmt.Errorf("el contenedor %q no existe", r.ContainerName)
	}
	if err != nil {
		return store.Server{}, err
	}
	im, ok := describe(info)
	if !ok {
		return store.Server{}, errors.New("el contenedor no parece ser SQL Server ni PostgreSQL")
	}
	servers, err := s.Store.ListServers()
	if err != nil {
		return store.Server{}, err
	}
	for _, sv := range servers {
		if sv.ContainerName == info.Name {
			return store.Server{}, errors.New("ese contenedor ya está registrado")
		}
		if strings.EqualFold(sv.Name, info.Name) {
			return store.Server{}, fmt.Errorf("ya existe un servidor llamado %q", info.Name)
		}
	}
	if !info.Running {
		return store.Server{}, errors.New("el contenedor está detenido; inícielo antes de importarlo para verificar la conexión")
	}
	if im.HostPort == 0 {
		return store.Server{}, errors.New("el contenedor no publica ningún puerto en el equipo; no se puede conectar")
	}
	user := strings.TrimSpace(r.AdminUser)
	if user == "" {
		user = "sa"
		if im.Engine == validate.EnginePostgres {
			user = "postgres"
		}
	}
	if r.AdminPassword == "" {
		return store.Server{}, errors.New("la contraseña de administrador es obligatoria")
	}
	conn := engine.Conn{Host: "localhost", Port: im.HostPort, User: user, Password: r.AdminPassword,
		TLS: im.Engine == validate.EngineMSSQL, TrustCert: true}
	eng, err := NewEngine(im.Engine, conn)
	if err != nil {
		return store.Server{}, err
	}
	defer eng.Close()
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := eng.Ping(pctx); err != nil {
		return store.Server{}, fmt.Errorf("no se pudo conectar con el usuario %q y esa contraseña: %s", user, CleanErr(err))
	}
	sv := store.Server{Name: info.Name, Engine: im.Engine, Mode: "local", Host: "localhost", Port: im.HostPort,
		AdminUser: user, ContainerName: info.Name, Image: info.Image, DataMode: im.DataMode, DataPath: im.DataPath}
	if im.Engine == validate.EnginePostgres {
		sv.PgDefaultDB = "postgres"
	}
	created, err := s.Store.CreateServer(sv, r.AdminPassword)
	s.logOp(created.ID, "import-container", info.Name, err)
	return created, err
}
