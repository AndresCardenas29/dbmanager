package services

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/engine"
	"dbmanager/internal/store"
	"dbmanager/internal/validate"
)

// CreateRequest son los datos para crear un servicio local.
type CreateRequest struct {
	RequestID       string
	Name            string
	Engine          string
	Image           string
	Port            int
	AdminPassword   string
	DataMode        string // volume | bind
	DataPath        string
	MemoryMB        int
	ExposeOnNetwork bool   // publicar en 0.0.0.0 (por defecto solo 127.0.0.1)
	Edition         string // mssql: Developer | Express ... (MSSQL_PID)
	// RootUser / RootPassword: usuario administrador que se crea en el motor y con el que DB Manager
	// se conecta desde entonces. Los resuelve el llamador (Ajustes > root predeterminado).
	RootUser     string
	RootPassword string
}

// ValidMSSQLEdition indica si la edición (MSSQL_PID) está permitida.
func ValidMSSQLEdition(e string) bool {
	switch strings.ToLower(e) {
	case "developer", "express", "standard", "enterprise", "web":
		return true
	}
	return false
}

// Validate normaliza y valida la petición (sin tocar Docker).
func (r *CreateRequest) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return errors.New("el nombre del servicio es obligatorio")
	}
	if len(r.Name) > 64 {
		return errors.New("el nombre del servicio es demasiado largo (máx. 64 caracteres)")
	}
	switch r.Engine {
	case validate.EngineMSSQL:
		if r.Image == "" {
			r.Image = DefaultMSSQLImage
		}
		if err := validate.SQLServerPassword(r.AdminPassword); err != nil {
			return err
		}
		if r.Edition == "" {
			r.Edition = "Developer"
		}
		if !ValidMSSQLEdition(r.Edition) {
			return fmt.Errorf("edición de SQL Server no válida: %s", r.Edition)
		}
	case validate.EnginePostgres:
		if r.Image == "" {
			r.Image = DefaultPostgresImage
		}
		if r.AdminPassword == "" {
			return errors.New("la contraseña de administrador es obligatoria")
		}
	default:
		return errors.New("motor inválido: use mssql o postgres")
	}
	r.RootUser = strings.TrimSpace(r.RootUser)
	if r.RootUser == "" {
		r.RootUser = store.DefaultRootUser
	}
	if r.RootPassword == "" {
		r.RootPassword = store.DefaultRootPassword
	}
	if err := ValidateRootUser(r.Engine, r.RootUser); err != nil {
		return err
	}
	if strings.ContainsAny(r.Image, " \t\n") {
		return errors.New("imagen inválida")
	}
	if r.Port < 0 || r.Port > 65535 {
		return errors.New("el puerto debe estar entre 1 y 65535")
	}
	if r.DataMode == "" {
		r.DataMode = "volume"
	}
	switch r.DataMode {
	case "volume":
		if r.DataPath != "" && !volumeNameRe.MatchString(r.DataPath) {
			return errors.New("nombre de volumen inválido (use letras, números, _ . -)")
		}
	case "bind":
		if r.DataPath == "" || !filepath.IsAbs(r.DataPath) {
			return errors.New("indique una carpeta absoluta del equipo para almacenar los datos")
		}
	default:
		return errors.New("modo de almacenamiento inválido: use volume o bind")
	}
	if r.MemoryMB < 0 {
		return errors.New("memoria inválida")
	}
	if r.Engine == validate.EngineMSSQL && r.MemoryMB > 0 && r.MemoryMB < 2048 {
		return errors.New("SQL Server necesita al menos 2048 MB de memoria")
	}
	return nil
}

// createMu serializa la reserva de puerto + creación/arranque entre creaciones simultáneas.
var createMu sync.Mutex

func defaultPort(engineName string) (start, container int) {
	if engineName == validate.EngineMSSQL {
		return 14331, 1433
	}
	return 54321, 5432
}

func (s *Services) progress(rid string, id int64, pct int, msg string) {
	s.emit("service:progress", Progress{ID: id, RequestID: rid, Percent: pct, Message: msg})
}

func (s *Services) fail(rid string, err error) error {
	s.emit("service:progress", Progress{RequestID: rid, Message: CleanErr(err), Done: true, Error: true})
	return err
}

// CreateLocal crea el contenedor, espera a que el motor acepte conexiones y registra el servicio.
func (s *Services) CreateLocal(ctx context.Context, r CreateRequest) (store.Server, error) {
	if err := r.Validate(); err != nil {
		return store.Server{}, err
	}
	rid := r.RequestID
	existing, err := s.Store.ListServers()
	if err != nil {
		return store.Server{}, err
	}
	for _, e := range existing {
		if strings.EqualFold(e.Name, r.Name) {
			return store.Server{}, errors.New("ya existe un servidor con ese nombre")
		}
	}
	cli, err := s.Docker()
	if err != nil {
		return store.Server{}, err
	}
	if err := cli.Ping(ctx); err != nil {
		return store.Server{}, errors.New("Docker no está disponible: inicie Docker Desktop (o el servicio docker) e inténtelo de nuevo")
	}
	slug := Slug(r.Name)
	cname := "dbmanager-" + slug
	if _, err := cli.Inspect(ctx, cname); err == nil {
		return store.Server{}, fmt.Errorf("ya existe un contenedor llamado %q en Docker", cname)
	} else if !dockerx.IsNotFound(err) {
		return store.Server{}, err
	}

	start, cport := defaultPort(r.Engine)
	isUsedBy := func(used map[int]bool) func(int) bool {
		return func(p int) bool { return used[p] || !hostPortFree(p) }
	}
	if r.Port != 0 { // comprobación rápida antes de descargar nada
		used, err := s.publishedPorts(ctx)
		if err != nil {
			return store.Server{}, err
		}
		if isUsedBy(used)(r.Port) {
			return store.Server{}, fmt.Errorf("el puerto %d ya está en uso; elija otro puerto o déjelo vacío para asignar uno automáticamente", r.Port)
		}
	}

	opts := dockerx.CreateOptions{Name: cname, Image: r.Image, Engine: r.Engine, ContainerPort: cport, ExposeOnNetwork: r.ExposeOnNetwork}
	platform := ""
	adminUser := "postgres"
	switch r.Engine {
	case validate.EngineMSSQL:
		platform = mssqlPlatform
		adminUser = "sa"
		opts.Platform = platform
		opts.Env = []string{"ACCEPT_EULA=Y", "MSSQL_SA_PASSWORD=" + r.AdminPassword, "MSSQL_PID=" + r.Edition}
		if r.MemoryMB > 0 {
			opts.Env = append(opts.Env, fmt.Sprintf("MSSQL_MEMORY_LIMIT_MB=%d", r.MemoryMB))
		}
		opts.MountTarget = "/var/opt/mssql"
		if r.DataMode == "bind" {
			// Docker Desktop: SQL Server no arranca con todo /var/opt/mssql en una carpeta del host; solo se monta data.
			opts.MountTarget = "/var/opt/mssql/data"
		}
	case validate.EnginePostgres:
		opts.Env = []string{"POSTGRES_PASSWORD=" + r.AdminPassword, "POSTGRES_USER=postgres"}
		opts.MemoryMB = r.MemoryMB
		opts.MountTarget = PostgresMountTarget(r.Image) // se ajusta tras el pull con PG_MAJOR
	}
	dataPath := r.DataPath
	if r.DataMode == "bind" {
		opts.BindPath = r.DataPath
		if r.Engine == validate.EngineMSSQL {
			opts.User = "0:0"
		}
	} else {
		if dataPath == "" {
			dataPath = "dbmanager-" + slug + "-data"
		}
		opts.VolumeName = dataPath
	}

	// Imagen
	s.progress(rid, 0, 2, "Comprobando imagen "+r.Image)
	ok, err := cli.ImageExists(ctx, r.Image)
	if err != nil {
		return store.Server{}, s.fail(rid, err)
	}
	if !ok {
		s.progress(rid, 0, 3, "Descargando imagen "+r.Image)
		last := -1
		err := cli.PullImage(ctx, r.Image, platform, func(p dockerx.PullProgress) {
			pct := 3 + p.Percent*55/100
			if pct != last {
				last = pct
				s.progress(rid, 0, pct, "Descargando imagen: "+p.Status)
			}
		})
		if err != nil {
			return store.Server{}, s.fail(rid, fmt.Errorf("no se pudo descargar la imagen %s: %w", r.Image, err))
		}
	}

	if r.Engine == validate.EnginePostgres {
		if env, err := cli.ImageEnv(ctx, r.Image); err == nil {
			if m := MountTargetFromMajor(env["PG_MAJOR"]); m != "" {
				opts.MountTarget = m
			}
		}
	}

	// Reserva de puerto + creación + arranque en exclusión mutua para que dos creaciones simultáneas no elijan el mismo puerto.
	createMu.Lock()
	locked := true
	unlock := func() {
		if locked {
			locked = false
			createMu.Unlock()
		}
	}
	defer unlock()
	used, err := s.publishedPorts(ctx)
	if err != nil {
		return store.Server{}, s.fail(rid, err)
	}
	isUsed := isUsedBy(used)
	port := r.Port
	if port == 0 {
		if port, err = PickPort(start, isUsed); err != nil {
			return store.Server{}, s.fail(rid, err)
		}
	} else if isUsed(port) {
		return store.Server{}, s.fail(rid, fmt.Errorf("el puerto %d ya está en uso; elija otro puerto o déjelo vacío para asignar uno automáticamente", port))
	}
	opts.HostPort = port

	s.progress(rid, 0, 60, "Creando contenedor "+cname)
	id, err := cli.CreateContainer(ctx, opts)
	if err != nil {
		return store.Server{}, s.fail(rid, WrapErr(fmt.Errorf("no se pudo crear el contenedor: %w", err)))
	}
	leave := func(err error) error {
		return s.fail(rid, fmt.Errorf("%w. El contenedor %q se dejó creado para diagnóstico (revise sus logs)", err, cname))
	}
	s.progress(rid, 0, 65, "Iniciando contenedor")
	if err := cli.Start(ctx, id); err != nil {
		if IsPortError(err) {
			// el puerto no se pudo publicar: no tiene sentido dejar el contenedor
			cli.Remove(context.Background(), id, true)
			return store.Server{}, s.fail(rid, WrapErr(fmt.Errorf("no se pudo iniciar el contenedor: %w", err)))
		}
		return store.Server{}, leave(WrapErr(fmt.Errorf("no se pudo iniciar el contenedor: %w", err)))
	}
	unlock()

	sv := store.Server{Name: r.Name, Engine: r.Engine, Mode: "local", Host: "localhost", Port: port, AdminUser: adminUser,
		ContainerName: cname, Image: r.Image, DataMode: r.DataMode, DataPath: dataPath, MemoryMB: r.MemoryMB}
	if r.Engine == validate.EngineMSSQL {
		sv.MssqlEdition = r.Edition
	} else {
		sv.PgDefaultDB = "postgres"
	}
	conn, _ := s.connForNew(sv, r.AdminPassword)
	eng, err := NewEngine(r.Engine, conn)
	if err != nil {
		return store.Server{}, leave(err)
	}
	defer eng.Close()
	timeout := 90 * time.Second
	if r.Engine == validate.EngineMSSQL {
		timeout = 150 * time.Second
	}
	s.progress(rid, 0, 70, "Esperando a que el motor acepte conexiones")
	if err := s.waitReady(ctx, cli, id, eng, timeout, func(pct int) { s.progress(rid, 0, pct, "Esperando a que el motor acepte conexiones") }); err != nil {
		return store.Server{}, leave(err)
	}

	// Usuario root: administrador propio de DB Manager, así no depende de sa/postgres ni de logins sin permisos.
	s.progress(rid, 0, 96, "Creando el usuario root "+r.RootUser)
	if err := eng.EnsureAdminLogin(ctx, r.RootUser, r.RootPassword); err != nil {
		return store.Server{}, leave(fmt.Errorf("no se pudo crear el usuario root %q: %s", r.RootUser, CleanErr(err)))
	}
	sv.AdminUser = r.RootUser
	if err := s.verifyAdmin(ctx, sv, r.RootPassword); err != nil {
		return store.Server{}, leave(err)
	}

	created, err := s.Store.CreateServer(sv, r.RootPassword)
	if err != nil {
		return store.Server{}, leave(fmt.Errorf("no se pudo guardar el servicio: %w", err))
	}
	s.logOp(created.ID, "create-service", r.Name, nil)
	s.emit("service:progress", Progress{ID: created.ID, RequestID: rid, Percent: 100, Message: "Servicio listo", Done: true})
	return created, nil
}

func (s *Services) connForNew(sv store.Server, password string) (engine.Conn, error) {
	return engine.Conn{Host: "localhost", Port: sv.Port, User: sv.AdminUser, Password: password,
		TLS: sv.Engine == validate.EngineMSSQL, TrustCert: true}, nil
}

// waitReady reintenta Ping hasta que el motor responde, el contenedor termina o vence el plazo.
func (s *Services) waitReady(ctx context.Context, cli *dockerx.Client, id string, eng engine.Engine, timeout time.Duration, tick func(int)) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for i := 0; ; i++ {
		pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		_, lastErr = eng.Ping(pctx)
		cancel()
		if lastErr == nil {
			return nil
		}
		if info, err := cli.Inspect(ctx, id); err == nil && !info.Running {
			logs, _ := cli.Logs(ctx, id, 15)
			return fmt.Errorf("el contenedor se detuvo durante el arranque (%s). Últimos logs:\n%s", info.State, strings.TrimSpace(logs))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("el motor no aceptó conexiones tras %d s (último error: %s)", int(timeout.Seconds()), CleanErr(lastErr))
		}
		if tick != nil {
			pct := 70 + i*2
			if pct > 95 {
				pct = 95
			}
			tick(pct)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
