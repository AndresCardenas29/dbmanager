package sftp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/store"

	"golang.org/x/crypto/ssh"
)

const (
	DefaultImage = "atmoz/sftp:alpine"
	defaultPort  = 2224
	uploadDirDef = "upload"
)

// Emitter envía eventos de progreso ("sftp:progress").
type Emitter func(event string, id int64, requestID string, percent int, message string, done, isErr bool)

// Manager orquesta servicios SFTP.
type Manager struct {
	Store   *store.Store
	Docker  func() (*dockerx.Client, error)
	Emit    Emitter
	BaseDir string // <appdata>/sftp

	mu sync.Mutex
}

func (m *Manager) emit(id int64, rid string, pct int, msg string, done, isErr bool) {
	if m.Emit != nil {
		m.Emit("sftp:progress", id, rid, pct, msg, done, isErr)
	}
}

func (m *Manager) baseOf(id int64) string { return filepath.Join(m.BaseDir, strconv.FormatInt(id, 10)) }

var slugRe = regexp.MustCompile(`[^a-z0-9_.-]+`)

func slug(n string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(n)), "-"), "-._")
	if s == "" {
		s = "sftp"
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

// CreateRequest son los datos de un servicio nuevo.
type CreateRequest struct {
	RequestID       string
	Name            string
	Image           string
	Port            int
	DataMode        string // volume | bind
	DataPath        string // carpeta del host (bind) o prefijo de volumen (opcional)
	ExposeOnNetwork bool
}

// Create crea un servicio gestionado (sin usuarios todavía) y lo inicia.
func (m *Manager) Create(ctx context.Context, r CreateRequest) (store.SftpService, error) {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" || len(r.Name) > 64 {
		return store.SftpService{}, errors.New("indique un nombre (máx. 64 caracteres)")
	}
	if r.Image == "" {
		r.Image = DefaultImage
	}
	if strings.ContainsAny(r.Image, " \t\n") {
		return store.SftpService{}, errors.New("imagen inválida")
	}
	if r.DataMode == "" {
		r.DataMode = "volume"
	}
	switch r.DataMode {
	case "bind":
		if r.DataPath == "" || !filepath.IsAbs(r.DataPath) {
			return store.SftpService{}, errors.New("indique una carpeta absoluta del equipo para los archivos")
		}
	case "volume":
	default:
		return store.SftpService{}, errors.New("modo de almacenamiento inválido: use volume o bind")
	}
	if r.Port < 0 || r.Port > 65535 {
		return store.SftpService{}, errors.New("el puerto debe estar entre 1 y 65535")
	}
	existing, err := m.Store.ListSftpServices()
	if err != nil {
		return store.SftpService{}, err
	}
	for _, e := range existing {
		if strings.EqualFold(e.Name, r.Name) {
			return store.SftpService{}, errors.New("ya existe un servicio SFTP con ese nombre")
		}
	}
	cli, err := m.Docker()
	if err != nil {
		return store.SftpService{}, err
	}
	if err := cli.Ping(ctx); err != nil {
		return store.SftpService{}, errors.New("Docker no está disponible: inicie Docker Desktop (o el servicio docker) e inténtelo de nuevo")
	}
	cname := "dbmanager-" + slug(r.Name)
	if _, err := cli.Inspect(ctx, cname); err == nil {
		return store.SftpService{}, fmt.Errorf("ya existe un contenedor llamado %q en Docker", cname)
	} else if !dockerx.IsNotFound(err) {
		return store.SftpService{}, err
	}

	m.emit(0, r.RequestID, 2, "Comprobando imagen "+r.Image, false, false)
	ok, err := cli.ImageExists(ctx, r.Image)
	if err != nil {
		return store.SftpService{}, m.fail(r.RequestID, err)
	}
	if !ok {
		last := -1
		err := cli.PullImage(ctx, r.Image, "", func(p dockerx.PullProgress) {
			if pct := 3 + p.Percent*60/100; pct != last {
				last = pct
				m.emit(0, r.RequestID, pct, "Descargando imagen: "+p.Status, false, false)
			}
		})
		if err != nil {
			return store.SftpService{}, m.fail(r.RequestID, fmt.Errorf("no se pudo descargar la imagen %s: %w", r.Image, err))
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	port := r.Port
	used, err := publishedPorts(ctx, cli)
	if err != nil {
		return store.SftpService{}, m.fail(r.RequestID, err)
	}
	isUsed := func(p int) bool { return used[p] || !hostPortFree(p) }
	if port == 0 {
		for p := defaultPort; p < defaultPort+1000; p++ {
			if !isUsed(p) {
				port = p
				break
			}
		}
		if port == 0 {
			return store.SftpService{}, m.fail(r.RequestID, errors.New("no se encontró un puerto libre"))
		}
	} else if isUsed(port) {
		return store.SftpService{}, m.fail(r.RequestID, fmt.Errorf("el puerto %d ya está en uso; elija otro o déjelo vacío para asignar uno automáticamente", port))
	}

	dataPath := r.DataPath
	if r.DataMode == "volume" && dataPath == "" {
		dataPath = "dbmanager-" + slug(r.Name) + "-data"
	}
	svc, err := m.Store.CreateSftpService(store.SftpService{Name: r.Name, Mode: "managed", ContainerName: cname, Image: r.Image,
		Host: "localhost", Port: port, DataMode: r.DataMode, DataPath: dataPath, ExposeOnNetwork: r.ExposeOnNetwork})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return store.SftpService{}, errors.New("ya existe un servicio SFTP con ese nombre")
		}
		return store.SftpService{}, m.fail(r.RequestID, err)
	}
	m.emit(svc.ID, r.RequestID, 65, "Creando contenedor "+cname, false, false)
	if err := m.recreateLocked(ctx, cli, svc); err != nil {
		// el registro se conserva para poder diagnosticar/eliminar
		return store.SftpService{}, m.fail(r.RequestID, fmt.Errorf("%w. El servicio quedó registrado; revise los logs o elimínelo", err))
	}
	m.emit(svc.ID, r.RequestID, 100, "Servicio listo", true, false)
	return svc, nil
}

func (m *Manager) fail(rid string, err error) error {
	m.emit(0, rid, 0, err.Error(), true, true)
	return err
}

// ---------- contenedor ----------

func publishedPorts(ctx context.Context, cli *dockerx.Client) (map[int]bool, error) {
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

func hostPortFree(p int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

func volumeName(svc store.SftpService, user string) string { return svc.DataPath + "-" + user }

// buildConfig reconstruye la configuración en disco a partir de la base de datos y devuelve usuarios.
func (m *Manager) buildConfig(svc store.SftpService) ([]UserCfg, ConfigPaths, error) {
	us, err := m.Store.ListSftpUsers(svc.ID)
	if err != nil {
		return nil, ConfigPaths{}, err
	}
	var cfgs []UserCfg
	for _, u := range us {
		ks, err := m.Store.ListSftpKeys(u.ID)
		if err != nil {
			return nil, ConfigPaths{}, err
		}
		c := UserCfg{Name: u.Name, UID: u.UID, PasswordHash: u.PasswordHash, UploadDir: u.UploadDir}
		for _, k := range ks {
			c.PublicKeys = append(c.PublicKeys, k.PublicKey)
		}
		cfgs = append(cfgs, c)
	}
	p, err := WriteConfig(m.baseOf(svc.ID), cfgs)
	return cfgs, p, err
}

// recreateLocked regenera la configuración y recrea el contenedor (atmoz solo lee users.conf al primer arranque).
func (m *Manager) recreateLocked(ctx context.Context, cli *dockerx.Client, svc store.SftpService) error {
	if svc.Mode != "managed" {
		return errors.New("los servicios importados no se pueden reconfigurar desde la app")
	}
	cfgs, p, err := m.buildConfig(svc)
	if err != nil {
		return err
	}
	opts := dockerx.CreateOptions{Name: svc.ContainerName, Image: svc.Image, Engine: "sftp", HostPort: svc.Port, ContainerPort: 22,
		ExposeOnNetwork: svc.ExposeOnNetwork}
	opts.Binds = append(opts.Binds,
		dockerx.Bind{Source: p.UsersConf, Target: "/etc/sftp/users.conf", ReadOnly: true},
		dockerx.Bind{Source: p.HostKeys, Target: "/etc/sftp-keys"},
		dockerx.Bind{Source: p.HooksDir, Target: "/etc/sftp.d", ReadOnly: true})
	for _, u := range cfgs {
		// atmoz hace `cat /home/<u>/.ssh/keys/*`: con la carpeta vacía el script aborta y no crea a los usuarios siguientes.
		if len(u.PublicKeys) > 0 {
			opts.Binds = append(opts.Binds, dockerx.Bind{Source: filepath.Join(p.KeysDir, u.Name), Target: "/home/" + u.Name + "/.ssh/keys", ReadOnly: true})
		}
		target := "/home/" + u.Name + "/" + u.UploadDir
		if svc.DataMode == "bind" {
			src := filepath.Join(svc.DataPath, u.Name)
			if err := os.MkdirAll(src, 0o777); err != nil {
				return fmt.Errorf("no se pudo crear la carpeta %s: %w", src, err)
			}
			os.Chmod(src, 0o777)
			opts.Binds = append(opts.Binds, dockerx.Bind{Source: src, Target: target})
		} else {
			opts.Volumes = append(opts.Volumes, dockerx.VolumeMount{Name: volumeName(svc, u.Name), Target: target})
		}
	}
	if _, err := cli.Inspect(ctx, svc.ContainerName); err == nil {
		if err := cli.Remove(ctx, svc.ContainerName, false); err != nil && !dockerx.IsNotFound(err) {
			return fmt.Errorf("no se pudo eliminar el contenedor anterior: %w", err)
		}
	}
	id, err := cli.CreateContainer(ctx, opts)
	if err != nil {
		return fmt.Errorf("no se pudo crear el contenedor: %w", err)
	}
	if err := cli.Start(ctx, id); err != nil {
		return fmt.Errorf("no se pudo iniciar el contenedor: %w", err)
	}
	if err := m.waitSSH(ctx, cli, id, svc.Port, 60*time.Second); err != nil {
		return err
	}
	return m.verifyUsers(ctx, cli, id, cfgs)
}

// verifyUsers comprueba que el entrypoint creó a todos los usuarios; si no, muestra el error de los logs.
func (m *Manager) verifyUsers(ctx context.Context, cli *dockerx.Client, id string, cfgs []UserCfg) error {
	for _, u := range cfgs {
		res, err := cli.Exec(ctx, id, dockerx.ExecOptions{Cmd: []string{"id", "-u", u.Name}})
		if err == nil && res.ExitCode == 0 {
			continue
		}
		logs, _ := cli.Logs(ctx, id, 40)
		return fmt.Errorf("el servidor SFTP arrancó pero no creó al usuario %q; mensajes del servidor: %s", u.Name, errorLines(logs))
	}
	return nil
}

// errorLines extrae de los logs las líneas que parecen errores (o las últimas si no hay ninguna).
func errorLines(logs string) string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(logs), "\n") {
		ll := strings.ToLower(l)
		if strings.Contains(ll, "no such file") || strings.Contains(ll, "can't open") || strings.Contains(ll, "error") ||
			strings.Contains(ll, "useradd") || strings.Contains(ll, "adduser") || strings.Contains(ll, "invalid") {
			out = append(out, strings.TrimSpace(l))
		}
	}
	if len(out) == 0 {
		ls := strings.Split(strings.TrimSpace(logs), "\n")
		if len(ls) > 5 {
			ls = ls[len(ls)-5:]
		}
		return strings.Join(ls, " | ")
	}
	return strings.Join(out, " | ")
}

func (m *Manager) waitSSH(ctx context.Context, cli *dockerx.Client, id string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
		if err == nil {
			c.SetReadDeadline(time.Now().Add(2 * time.Second))
			buf := make([]byte, 4)
			n, _ := c.Read(buf)
			c.Close()
			if n == 4 && string(buf) == "SSH-" {
				return nil
			}
		}
		if info, ierr := cli.Inspect(ctx, id); ierr == nil && !info.Running {
			logs, _ := cli.Logs(ctx, id, 15)
			return fmt.Errorf("el contenedor se detuvo durante el arranque. Últimos logs:\n%s", strings.TrimSpace(logs))
		}
		if time.Now().After(deadline) {
			return errors.New("el servicio SSH no respondió a tiempo")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (m *Manager) recreate(ctx context.Context, svcID int64) error {
	svc, err := m.Store.GetSftpService(svcID)
	if err != nil {
		return err
	}
	cli, err := m.Docker()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.recreateLocked(ctx, cli, svc); err != nil {
		return fmt.Errorf("los cambios se guardaron pero no se pudo recrear el contenedor: %w", err)
	}
	return nil
}

// ---------- ciclo de vida ----------

func (m *Manager) get(id int64) (store.SftpService, *dockerx.Client, error) {
	svc, err := m.Store.GetSftpService(id)
	if err != nil {
		return svc, nil, errors.New("el servicio SFTP no existe")
	}
	cli, err := m.Docker()
	return svc, cli, err
}

func (m *Manager) Start(ctx context.Context, id int64) error {
	svc, cli, err := m.get(id)
	if err != nil {
		return err
	}
	return cli.Start(ctx, svc.ContainerName)
}

func (m *Manager) Stop(ctx context.Context, id int64) error {
	svc, cli, err := m.get(id)
	if err != nil {
		return err
	}
	return cli.Stop(ctx, svc.ContainerName)
}

func (m *Manager) Restart(ctx context.Context, id int64) error {
	svc, cli, err := m.get(id)
	if err != nil {
		return err
	}
	return cli.Restart(ctx, svc.ContainerName)
}

func (m *Manager) Logs(ctx context.Context, id int64, tail int) (string, error) {
	svc, cli, err := m.get(id)
	if err != nil {
		return "", err
	}
	return cli.Logs(ctx, svc.ContainerName, tail)
}

// Status devuelve estado (running|stopped|missing) del contenedor.
func (m *Manager) Status(ctx context.Context, id int64) (state string, msg string, err error) {
	svc, cli, err := m.get(id)
	if err != nil {
		return "", "", err
	}
	info, err := cli.Inspect(ctx, svc.ContainerName)
	if dockerx.IsNotFound(err) {
		return "missing", fmt.Sprintf("El contenedor %q no existe en Docker.", svc.ContainerName), nil
	}
	if err != nil {
		return "missing", "No se pudo consultar Docker: " + err.Error(), nil
	}
	if info.Running {
		return "running", info.Status, nil
	}
	return "stopped", info.Status, nil
}

// Delete elimina el servicio. En servicios importados solo se quita el registro (nunca se toca su contenedor).
// removeData borra los volúmenes; las carpetas del host (bind) nunca se borran.
func (m *Manager) Delete(ctx context.Context, id int64, removeContainer, removeData bool) error {
	svc, cli, err := m.get(id)
	if err != nil {
		return err
	}
	if svc.Mode == "managed" {
		if removeContainer {
			if err := cli.Remove(ctx, svc.ContainerName, false); err != nil && !dockerx.IsNotFound(err) {
				return fmt.Errorf("no se pudo eliminar el contenedor: %w", err)
			}
			if removeData && svc.DataMode == "volume" {
				// todos los volúmenes del servicio, incluidos los de usuarios eliminados con deleteFiles=false
				vols, err := cli.ListVolumes(ctx, svc.DataPath+"-")
				if err != nil {
					return err
				}
				for _, v := range vols {
					if err := cli.RemoveVolume(ctx, v); err != nil && !dockerx.IsNotFound(err) {
						return fmt.Errorf("no se pudo eliminar el volumen %s: %w", v, err)
					}
				}
			}
		}
		os.RemoveAll(m.baseOf(svc.ID))
	}
	return m.Store.DeleteSftpService(id)
}

// ---------- huellas ----------

// HostKeyLines devuelve las llaves de host de un servicio como líneas known_hosts y las huellas.
func (m *Manager) HostKeys(id int64) (knownHosts []string, fingerprints []string, err error) {
	svc, err := m.Store.GetSftpService(id)
	if err != nil {
		return nil, nil, errors.New("el servicio SFTP no existe")
	}
	host := svc.Host
	if host == "" {
		host = "localhost"
	}
	prefix := host
	if svc.Port != 22 {
		prefix = fmt.Sprintf("[%s]:%d", host, svc.Port)
	}
	var keys []ssh.PublicKey
	if svc.Mode == "managed" {
		for _, f := range []struct{ file, label string }{{"ssh_host_ed25519_key.pub", "ED25519"}, {"ssh_host_rsa_key.pub", "RSA"}} {
			b, rerr := os.ReadFile(filepath.Join(Paths(m.baseOf(svc.ID)).HostKeys, f.file))
			if rerr != nil {
				continue
			}
			if pub, _, _, _, perr := ssh.ParseAuthorizedKey(b); perr == nil {
				keys = append(keys, pub)
			}
		}
	} else {
		keys, err = ScanHostKeys(host, svc.Port)
		if err != nil {
			return nil, nil, err
		}
	}
	if len(keys) == 0 {
		return nil, nil, errors.New("no hay llaves de host disponibles")
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Type() < keys[j].Type() })
	for _, k := range keys {
		knownHosts = append(knownHosts, prefix+" "+AuthorizedLine(k, ""))
		label := KeyLabel(k)
		if i := strings.Index(label, " "); i > 0 {
			label = label[:i]
		}
		fingerprints = append(fingerprints, label+" "+ssh.FingerprintSHA256(k))
	}
	return knownHosts, fingerprints, nil
}

// ---------- importación ----------

// IsSftpContainer aplica las heurísticas de detección de contenedores SFTP.
func IsSftpContainer(info dockerx.ContainerInfo) bool {
	if strings.Contains(strings.ToLower(info.Image), "atmoz/sftp") {
		return true
	}
	for _, e := range info.EnvNames {
		if e == "SFTP_USERS" {
			return true
		}
	}
	for _, mnt := range info.Mounts {
		if mnt.Destination == "/etc/sftp/users.conf" {
			return true
		}
	}
	has22 := false
	for _, p := range info.Ports {
		if p.ContainerPort == 22 {
			has22 = true
		}
	}
	for _, e := range info.Entrypoint {
		if e == "/entrypoint" && has22 {
			return true
		}
	}
	return false
}

// Importable es un contenedor SFTP no registrado.
type Importable struct {
	ID, Name, Image, State, Status string
	HostPort, ContainerPort        int
}

// ListImportable lista contenedores SFTP no registrados (y que no son de la app).
func (m *Manager) ListImportable(ctx context.Context) ([]Importable, error) {
	cli, err := m.Docker()
	if err != nil {
		return nil, err
	}
	reg, err := m.Store.ListSftpServices()
	if err != nil {
		return nil, err
	}
	registered := map[string]bool{}
	for _, s := range reg {
		registered[s.ContainerName] = true
	}
	list, err := cli.ListContainers(ctx, true, "")
	if err != nil {
		return nil, err
	}
	out := []Importable{}
	for _, c := range list {
		if registered[c.Name] || c.Labels["dbmanager.engine"] == "sftp" || c.Labels["dbmanager.temp"] == "true" {
			continue
		}
		info, err := cli.Inspect(ctx, c.ID)
		if err != nil || !IsSftpContainer(info) {
			continue
		}
		im := Importable{ID: info.ID, Name: info.Name, Image: info.Image, State: info.State, Status: info.Status, ContainerPort: 22}
		for _, p := range info.Ports {
			if p.ContainerPort == 22 {
				im.HostPort = p.HostPort
			}
		}
		out = append(out, im)
	}
	return out, nil
}

// Import registra un contenedor SFTP existente en modo external (solo lectura/gestión de ciclo de vida).
func (m *Manager) Import(ctx context.Context, containerName string) (store.SftpService, error) {
	containerName = strings.TrimSpace(containerName)
	cli, err := m.Docker()
	if err != nil {
		return store.SftpService{}, err
	}
	info, err := cli.Inspect(ctx, containerName)
	if dockerx.IsNotFound(err) {
		return store.SftpService{}, fmt.Errorf("el contenedor %q no existe", containerName)
	}
	if err != nil {
		return store.SftpService{}, err
	}
	if !IsSftpContainer(info) {
		return store.SftpService{}, errors.New("el contenedor no parece ser un servidor SFTP")
	}
	reg, err := m.Store.ListSftpServices()
	if err != nil {
		return store.SftpService{}, err
	}
	for _, s := range reg {
		if s.ContainerName == info.Name {
			return store.SftpService{}, errors.New("ese contenedor ya está registrado")
		}
		if strings.EqualFold(s.Name, info.Name) {
			return store.SftpService{}, fmt.Errorf("ya existe un servicio llamado %q", info.Name)
		}
	}
	port, expose := 0, false
	for _, p := range info.Ports {
		if p.ContainerPort == 22 {
			port = p.HostPort
			expose = p.HostIP == "0.0.0.0" || p.HostIP == ""
		}
	}
	svc, err := m.Store.CreateSftpService(store.SftpService{Name: info.Name, Mode: "external", ContainerName: info.Name, Image: info.Image,
		Host: "localhost", Port: port, ExposeOnNetwork: expose})
	if err != nil {
		return store.SftpService{}, err
	}
	return svc, nil
}
