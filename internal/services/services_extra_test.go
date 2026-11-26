package services

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/store"
)

type evCollector struct {
	mu  sync.Mutex
	evs map[string][]Progress
}

func (c *evCollector) emit(ev string, p Progress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.evs == nil {
		c.evs = map[string][]Progress{}
	}
	c.evs[ev] = append(c.evs[ev], p)
}

// svcNoDocker construye Services sin Docker, con un store temporal.
func svcNoDocker(t *testing.T) (*Services, *evCollector) {
	t.Helper()
	st, err := store.OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	col := &evCollector{}
	return &Services{Store: st, Docker: func() (*dockerx.Client, error) { return nil, errors.New("sin docker") }, Emit: col.emit}, col
}

func closedPort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}

func remoteServer(t *testing.T, s *Services, engine string, port int) store.Server {
	t.Helper()
	sv, err := s.Store.CreateServer(store.Server{Name: "r-" + engine, Engine: engine, Mode: "remote", Host: "127.0.0.1", Port: port,
		AdminUser: "u", PgDefaultDB: "postgres"}, "Some!Passw0rd")
	if err != nil {
		t.Fatal(err)
	}
	return sv
}

func checkProgress(t *testing.T, evs []Progress, rid string, wantID int64) {
	t.Helper()
	if len(evs) < 2 {
		t.Fatalf("se esperaban >=2 eventos, hubo %d", len(evs))
	}
	for i, e := range evs {
		if e.RequestID != rid {
			t.Errorf("evento %d sin requestId correcto: %+v", i, e)
		}
		if wantID != 0 && e.ID != wantID {
			t.Errorf("evento %d con id %d, esperaba %d", i, e.ID, wantID)
		}
	}
	last := evs[len(evs)-1]
	if !last.Done {
		t.Errorf("el último evento debe tener Done: %+v", last)
	}
}

// El fallo de un backup emite eventos con requestId y un evento final Done+Error; el registro queda en error.
func TestBackupFailureEventsAndRecord(t *testing.T) {
	s, col := svcNoDocker(t)
	sv := remoteServer(t, s, "postgres", closedPort(t))
	rec, err := s.Backup(context.Background(), sv, "midb", t.TempDir(), "rid-bk")
	if err == nil {
		t.Fatal("debía fallar: no hay servidor")
	}
	if rec.ID == 0 {
		t.Fatal("debía existir un registro de backup")
	}
	checkProgress(t, col.evs["backup:progress"], "rid-bk", rec.ID)
	last := col.evs["backup:progress"][len(col.evs["backup:progress"])-1]
	if !last.Error {
		t.Errorf("el último evento debe ser de error: %+v", last)
	}
	got, _ := s.Store.GetBackup(rec.ID)
	if got.Status != "error" || got.Message == "" {
		t.Fatalf("registro: %+v", got)
	}
}

func TestBackupValidation(t *testing.T) {
	s, col := svcNoDocker(t)
	sv := remoteServer(t, s, "postgres", closedPort(t))
	ctx := context.Background()
	for name, c := range map[string]struct{ db, dir string }{
		"sin base": {"", t.TempDir()}, "sin destino": {"x", ""}, "destino relativo": {"x", "relativo/dir"},
	} {
		if _, err := s.Backup(ctx, sv, c.db, c.dir, "r"); err == nil {
			t.Errorf("%s: debía fallar", name)
		} else if strings.Contains(err.Error(), "connect") {
			t.Errorf("%s: error inesperado %v", name, err)
		}
	}
	if len(col.evs["backup:progress"]) != 0 {
		t.Error("validaciones previas no deben crear registro ni emitir eventos")
	}
	if l, _ := s.Store.ListBackups(0); len(l) != 0 {
		t.Errorf("no debe haber registros: %+v", l)
	}
}

func TestRestoreFailureEventsAndRecord(t *testing.T) {
	s, col := svcNoDocker(t)
	sv := remoteServer(t, s, "postgres", closedPort(t))
	f := filepath.Join(t.TempDir(), "x.sql")
	os.WriteFile(f, []byte("select 1;"), 0o600)
	rec, err := s.Restore(context.Background(), sv, RestoreRequest{RequestID: "rid-rs", FilePath: f, TargetDatabase: "destino"})
	if err == nil {
		t.Fatal("debía fallar")
	}
	checkProgress(t, col.evs["restore:progress"], "rid-rs", rec.ID)
	got, _ := s.Store.GetRestore(rec.ID)
	if got.Status != "error" || got.FinishedAt.IsZero() || got.Message == "" {
		t.Fatalf("registro: %+v", got)
	}
}

func TestRestoreValidation(t *testing.T) {
	s, col := svcNoDocker(t)
	sv := remoteServer(t, s, "postgres", closedPort(t))
	f := filepath.Join(t.TempDir(), "x.sql")
	os.WriteFile(f, []byte("select 1;"), 0o600)
	ctx := context.Background()
	cases := map[string]RestoreRequest{
		"destino con ]":     {FilePath: f, TargetDatabase: "a]b"},
		"destino inyección": {FilePath: f, TargetDatabase: "x'; DROP DATABASE y; --"},
		"destino vacío":     {FilePath: f, TargetDatabase: ""},
		"sin archivo":       {FilePath: "", TargetDatabase: "ok"},
		"archivo inexist.":  {FilePath: filepath.Join(t.TempDir(), "no.sql"), TargetDatabase: "ok"},
		"carpeta":           {FilePath: t.TempDir(), TargetDatabase: "ok"},
	}
	for name, r := range cases {
		if _, err := s.Restore(ctx, sv, r); err == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
	if len(col.evs["restore:progress"]) != 0 {
		t.Error("validaciones previas no deben emitir eventos")
	}
	if l, _ := s.Store.ListRestores(0); len(l) != 0 {
		t.Errorf("no deben crearse registros: %+v", l)
	}
}

func TestCreateLocalNoDockerReportsSpanish(t *testing.T) {
	s, _ := svcNoDocker(t)
	_, err := s.CreateLocal(context.Background(), CreateRequest{Name: "x", Engine: "postgres", AdminPassword: "p"})
	if err == nil {
		t.Fatal("debía fallar sin Docker")
	}
}

func TestCreateRequestValidateMore(t *testing.T) {
	bad := map[string]CreateRequest{
		"puerto negativo":      {Name: "x", Engine: "postgres", AdminPassword: "p", Port: -1},
		"puerto grande":        {Name: "x", Engine: "postgres", AdminPassword: "p", Port: 70000},
		"modo inválido":        {Name: "x", Engine: "postgres", AdminPassword: "p", DataMode: "nfs"},
		"volumen con espacio":  {Name: "x", Engine: "postgres", AdminPassword: "p", DataPath: "a b"},
		"volumen con ruta":     {Name: "x", Engine: "postgres", AdminPassword: "p", DataPath: "../etc"},
		"edición inválida":     {Name: "x", Engine: "mssql", AdminPassword: "Str0ng!Pass", Edition: "Gold"},
		"mssql poca memoria":   {Name: "x", Engine: "mssql", AdminPassword: "Str0ng!Pass", MemoryMB: 512},
		"memoria negativa":     {Name: "x", Engine: "postgres", AdminPassword: "p", MemoryMB: -5},
		"imagen con espacio":   {Name: "x", Engine: "postgres", AdminPassword: "p", Image: "postgres 17"},
		"nombre largo":         {Name: strings.Repeat("a", 65), Engine: "postgres", AdminPassword: "p"},
		"pg sin contraseña":    {Name: "x", Engine: "postgres"},
		"nombre solo espacios": {Name: "   ", Engine: "postgres", AdminPassword: "p"},
	}
	for name, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
	r := CreateRequest{Name: " ok ", Engine: "postgres", AdminPassword: "p", Edition: "whatever"}
	if err := r.Validate(); err != nil || r.Name != "ok" {
		t.Errorf("%v %+v", err, r)
	}
}

// Los mensajes de validación de la petición están en español.
func TestCreateRequestMessagesSpanish(t *testing.T) {
	for _, r := range []CreateRequest{
		{Name: "", Engine: "postgres", AdminPassword: "p"},
		{Name: "x", Engine: "oracle", AdminPassword: "p"},
		{Name: "x", Engine: "mssql", AdminPassword: "weak"},
		{Name: "x", Engine: "postgres", AdminPassword: "p", DataMode: "bind", DataPath: "rel"},
	} {
		err := r.Validate()
		if err == nil {
			t.Fatal("debía fallar")
		}
		for _, en := range []string{"invalid", "required", "must", "cannot"} {
			if strings.Contains(strings.ToLower(err.Error()), en) {
				t.Errorf("mensaje en inglés: %v", err)
			}
		}
	}
}

func TestDescribeEdgeCases(t *testing.T) {
	// sin puertos publicados: se importa pero HostPort=0 (Import lo rechaza después)
	im, ok := describe(dockerx.ContainerInfo{Name: "p", Image: "postgres:17"})
	if !ok || im.HostPort != 0 || im.ContainerPort != 5432 {
		t.Fatalf("%+v", im)
	}
	// puerto en otro contenedor-puerto: usa el primero publicado
	im, _ = describe(dockerx.ContainerInfo{Name: "p", Image: "postgres:17", Ports: []dockerx.PortMapping{{HostPort: 6543, ContainerPort: 6000}}})
	if im.HostPort != 6543 || im.ContainerPort != 6000 {
		t.Fatalf("%+v", im)
	}
	// montaje de volumen
	im, _ = describe(dockerx.ContainerInfo{Name: "p", Image: "postgres:17", Mounts: []dockerx.MountInfo{{Type: "volume", Name: "vol1", Destination: "/var/lib/postgresql/data"}}})
	if im.DataMode != "volume" || im.DataPath != "vol1" {
		t.Fatalf("%+v", im)
	}
	// una imagen cuyo nombre solo contiene "postgres" en el tag no debe confundirse
	if DetectEngine("myorg/app:postgres-client", nil, nil, nil, nil) != "" {
		t.Error("falso positivo postgres por tag")
	}
	if DetectEngine("nginx", nil, nil, []string{"PGHOST"}, nil) != "" {
		t.Error("falso positivo")
	}
}

func TestPickPortSkipsUsedAndBounds(t *testing.T) {
	if p, err := PickPort(65535, func(p int) bool { return false }); err != nil || p != 65535 {
		t.Fatal(p, err)
	}
	if _, err := PickPort(65535, func(p int) bool { return true }); err == nil {
		t.Fatal("debe fallar")
	}
	calls := 0
	PickPort(1000, func(p int) bool { calls++; return true })
	if calls > 1000 {
		t.Fatalf("más de 1000 intentos: %d", calls)
	}
}

// BUG (media/baja): PostgresMountTarget interpreta mal tags nombrados/digest.
//   - "postgres:bookworm"/"postgres:alpine" son equivalentes a "latest" (PG 18+) y requieren /var/lib/postgresql.
//   - "postgres@sha256:18ab..." se interpreta con los primeros dígitos del hash.
func TestPostgresMountTargetNamedTags(t *testing.T) {
	cases := map[string]string{
		"postgres:bookworm":        "/var/lib/postgresql",
		"postgres:alpine":          "/var/lib/postgresql",
		"postgres:trixie":          "/var/lib/postgresql",
		"postgres@sha256:9f3a0000": "/var/lib/postgresql", // no se puede conocer la versión; lo seguro es no usar /data
		"postgres:18.1":            "/var/lib/postgresql",
		"postgres:9.6":             "/var/lib/postgresql/data",
		"postgres:17-alpine3.20":   "/var/lib/postgresql/data",
	}
	for img, want := range cases {
		if got := PostgresMountTarget(img); got != want {
			t.Errorf("%s -> %s, esperaba %s", img, got, want)
		}
	}
}

func TestPostgresMountTargetNumericTags(t *testing.T) {
	cases := map[string]string{
		"postgres:17":     "/var/lib/postgresql/data",
		"postgres:17.2":   "/var/lib/postgresql/data",
		"postgres:18":     "/var/lib/postgresql",
		"postgres:19":     "/var/lib/postgresql",
		"postgres:9.6":    "/var/lib/postgresql/data",
		"postgres:latest": "/var/lib/postgresql", "postgres": "/var/lib/postgresql",
		"localhost:5000/postgres:16": "/var/lib/postgresql/data",
		"postgres:16-alpine":         "/var/lib/postgresql/data",
	}
	for img, want := range cases {
		if got := PostgresMountTarget(img); got != want {
			t.Errorf("%s -> %s, esperaba %s", img, got, want)
		}
	}
}

func TestSlugEdgeCases(t *testing.T) {
	long := strings.Repeat("ab", 60)
	if s := Slug(long); len(s) > 48 {
		t.Error("slug largo")
	}
	for in, want := range map[string]string{"ÑANDÚ": "and", "  A B  ": "a-b", "x_y.z-w": "x_y.z-w", "../etc": "etc", "a/b\\c": "a-b-c"} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q)=%q, esperaba %q", in, got, want)
		}
	}
	// el nombre del contenedor nunca contiene caracteres peligrosos
	for _, in := range []string{"x; rm -rf /", "$(id)", "a b`c", "`id`"} {
		if strings.ContainsAny(Slug(in), " ;$()`\\/") {
			t.Errorf("Slug(%q) inseguro: %q", in, Slug(in))
		}
	}
}

func TestBackupFileNameSafe(t *testing.T) {
	for _, db := range []string{"a]b", "x'; DROP", "../../etc/passwd", `a\b`, "ñ", "a b"} {
		n := BackupFileName(db, "bak", time.Unix(0, 0))
		if strings.ContainsAny(n, `/\ ';]`) || strings.Contains(n, "..") {
			t.Errorf("nombre de archivo inseguro para %q: %q", db, n)
		}
	}
}

func TestDockerHostFor(t *testing.T) {
	for in, want := range map[string]string{"": "host.docker.internal", "LOCALHOST": "host.docker.internal", "127.0.0.1": "host.docker.internal",
		"::1": "host.docker.internal", "10.0.0.5": "10.0.0.5", "db.corp": "db.corp"} {
		if got := dockerHostFor(in); got != want {
			t.Errorf("%q -> %q", in, got)
		}
	}
}

func TestCleanErrTruncates(t *testing.T) {
	if len(CleanErr(errors.New(strings.Repeat("x", 5000)))) > 610 {
		t.Error("CleanErr debe truncar")
	}
	if CleanErr(nil) != "" {
		t.Error("nil")
	}
}
