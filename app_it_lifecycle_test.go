//go:build integration

package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dbmanager/internal/testutil"
)

// localNames devuelve el nombre de servicio (test-<tag>-<hex>) con el nombre de contenedor y volumen resultantes
// (dbmanager-test-...), y registra la limpieza.
func localNames(t *testing.T, tag string) (svcName, cname, vol string) {
	svcName = "test-" + tag + "-" + uniq("x")[2:]
	cname = "dbmanager-" + svcName
	vol = cname + "-data"
	t.Cleanup(func() {
		testutil.RemoveByName(cname)
		testutil.RemoveVolume(vol)
	})
	return
}

func waitReachable(t *testing.T, a *App, id int64, d time.Duration) {
	t.Helper()
	waitUntil(t, d, "servidor accesible", func() bool {
		st, err := a.TestConnection(id)
		return err == nil && st.Reachable
	})
}

// Crear servicio local, estados Start/Stop/Restart, persistencia y eventos de progreso.
func TestLocalServiceLifecycle(t *testing.T) {
	a, evlog := newTestApp(t)
	name, cname, vol := localNames(t, "lc")
	const pw = "Lc!Passw0rd-9"

	// puerto ocupado explícito: error claro y no se crea nada
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	busy := l.Addr().(*net.TCPAddr).Port
	_, err = a.CreateLocalService(CreateServiceRequest{RequestID: "busy", Name: name, Engine: "postgres", Port: busy, AdminPassword: pw})
	l.Close()
	if err == nil || !strings.Contains(err.Error(), "ya está en uso") {
		t.Errorf("puerto ocupado: %v", err)
	}
	if testutil.ContainerExists(cname) {
		t.Fatal("no debió crearse el contenedor con el puerto ocupado")
	}

	evlog.reset()
	sv, err := a.CreateLocalService(CreateServiceRequest{RequestID: "lc-1", Name: name, Engine: "postgres", AdminPassword: pw})
	if err != nil {
		t.Fatalf("CreateLocalService: %v", err)
	}
	if sv.ContainerName != cname || sv.Mode != "local" || sv.Engine != "postgres" || sv.DataMode != "volume" || sv.DataPath != vol ||
		!sv.HasPassword || sv.AdminUser != "root" || sv.Port < 54321 {
		t.Errorf("servidor creado (se espera el usuario root predeterminado): %+v", sv)
	}
	if st, err := a.GetRootStatus(sv.ID); err != nil || !st.IsAdmin {
		t.Errorf("el usuario root del servicio nuevo debe ser administrador: %+v %v", st, err)
	}
	noSecret(t, "Server", mustJSON(t, sv), pw)
	// eventos: todos con requestId; porcentajes no decrecientes; último Done
	evs := evlog.of("service:progress")
	if len(evs) < 4 {
		t.Errorf("pocos eventos: %d", len(evs))
	}
	prev := -1
	for i, e := range evs {
		if e.RequestID != "lc-1" {
			t.Errorf("evento %d sin requestId: %+v", i, e)
		}
		if e.Percent < prev {
			t.Errorf("porcentaje retrocede en %d: %d -> %d", i, prev, e.Percent)
		}
		prev = e.Percent
	}
	if last := evs[len(evs)-1]; !last.Done || last.Error || last.ID != sv.ID || last.Percent != 100 {
		t.Errorf("último evento: %+v", last)
	}
	// el puerto asignado es el real y está publicado
	if _, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(sv.Port), 3*time.Second); err != nil {
		t.Errorf("el puerto %d no responde: %v", sv.Port, err)
	}
	// ya no es importable; duplicados rechazados (también con otra capitalización)
	imp, _ := a.ListImportableContainers()
	for _, ci := range imp {
		if ci.Name == cname {
			t.Error("el contenedor registrado aparece como importable")
		}
	}
	for _, dup := range []string{name, strings.ToUpper(name)} {
		if _, err := a.CreateLocalService(CreateServiceRequest{Name: dup, Engine: "postgres", AdminPassword: pw}); err == nil ||
			!strings.Contains(err.Error(), "ya existe") {
			t.Errorf("nombre duplicado %q: %v", dup, err)
		}
	}

	// datos que deben sobrevivir al reinicio
	waitReachable(t, a, sv.ID, 30*time.Second)
	if err := a.CreateDatabase(sv.ID, "persist_db", ""); err != nil {
		t.Fatal(err)
	}
	if err := a.CreateLogin(sv.ID, CreateLoginRequest{Name: "persist_user", Password: "Pw!12345678"}); err != nil {
		t.Fatal(err)
	}

	status := func() ServerStatus {
		s, err := a.ServerStatus(sv.ID)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := status(); s.State != "running" {
		t.Errorf("estado inicial: %+v", s)
	}
	// Stop
	if err := a.StopServer(sv.ID); err != nil {
		t.Fatal(err)
	}
	if s := status(); s.State != "stopped" {
		t.Errorf("tras Stop: %+v", s)
	}
	if err := a.StopServer(sv.ID); err != nil {
		t.Errorf("Stop repetido no debería fallar: %v", err)
	}
	// con el servicio detenido, las operaciones dan un error claro en español (no un timeout)
	start := time.Now()
	if _, err := a.ListDatabases(sv.ID); err == nil || !strings.Contains(err.Error(), "detenido") {
		t.Errorf("ListDatabases detenido: %v", err)
	}
	if time.Since(start) > 15*time.Second {
		t.Error("el error de servicio detenido tardó demasiado")
	}
	if st, _ := a.TestConnection(sv.ID); st.State != "stopped" || st.Reachable {
		t.Errorf("TestConnection detenido: %+v", st)
	}
	if _, err := a.CreateBackup(BackupRequest{ServerID: sv.ID, Database: "persist_db", DestDir: t.TempDir()}); err == nil ||
		!strings.Contains(err.Error(), "detenido") {
		t.Errorf("backup con servicio detenido: %v", err)
	}
	// Start
	if err := a.StartServer(sv.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.StartServer(sv.ID); err != nil {
		t.Errorf("Start repetido no debería fallar: %v", err)
	}
	if s := status(); s.State != "running" {
		t.Errorf("tras Start: %+v", s)
	}
	waitReachable(t, a, sv.ID, 60*time.Second)
	// Restart
	if err := a.RestartServer(sv.ID); err != nil {
		t.Fatal(err)
	}
	waitReachable(t, a, sv.ID, 60*time.Second)
	dbs, err := a.ListDatabases(sv.ID)
	if err != nil || !hasDB(dbs, "persist_db") {
		t.Errorf("los datos no persistieron al reiniciar: %v %+v", err, dbs)
	}
	lgs, _ := a.ListLogins(sv.ID)
	if !hasLogin(lgs, "persist_user") {
		t.Error("el login no persistió")
	}
	// logs
	logs, err := a.ServerLogs(sv.ID, 20)
	if err != nil || strings.TrimSpace(logs) == "" {
		t.Errorf("ServerLogs: %q %v", logs, err)
	}
	// operaciones registradas
	ops, _ := a.ListOperations(100)
	kinds := map[string]bool{}
	for _, o := range ops {
		kinds[o.Kind] = true
	}
	for _, k := range []string{"stop", "start", "restart", "create-database", "create-login"} {
		if !kinds[k] {
			t.Errorf("falta la operación %q en el historial", k)
		}
	}
	noSecret(t, "operaciones", mustJSON(t, ops), pw, "Pw!12345678")

	// contenedor eliminado por fuera -> missing, y Start da un mensaje claro
	testutil.RemoveByName(cname)
	if s := status(); s.State != "missing" {
		t.Errorf("tras eliminar por fuera: %+v", s)
	}
	if err := a.StartServer(sv.ID); err == nil || !strings.Contains(err.Error(), "no existe") {
		t.Errorf("Start de contenedor inexistente: %v", err)
	}
	if _, err := a.ServerLogs(sv.ID, 5); err == nil || !strings.Contains(err.Error(), "no existe") {
		t.Errorf("logs de contenedor inexistente: %v", err)
	}
	if _, err := a.ListDatabases(sv.ID); err == nil || !strings.Contains(err.Error(), "no existe") {
		t.Errorf("ListDatabases de contenedor inexistente: %v", err)
	}
	// el volumen sigue ahí; DeleteServer con removeContainer sobre un contenedor ya inexistente no falla
	if !testutil.VolumeExists(vol) {
		t.Error("el volumen debía conservarse")
	}
	if err := a.DeleteServer(sv.ID, true, true); err != nil {
		t.Errorf("DeleteServer sobre contenedor inexistente: %v", err)
	}
	if testutil.VolumeExists(vol) {
		t.Error("removeData=true debía eliminar el volumen")
	}
	if _, err := a.GetServer(sv.ID); err == nil {
		t.Error("el servidor debía eliminarse del registro")
	}
}

// Semántica de DeleteServer en modo volumen (conservar / eliminar datos) y re-importación del contenedor conservado.
func TestDeleteServerVolumeModes(t *testing.T) {
	a, _ := newTestApp(t)
	name, cname, vol := localNames(t, "dv")
	const pw = "Dv!Passw0rd-9"
	sv, err := a.CreateLocalService(CreateServiceRequest{Name: name, Engine: "postgres", AdminPassword: pw})
	if err != nil {
		t.Fatal(err)
	}
	if sv.DataMode != "volume" || sv.DataPath != vol {
		t.Fatalf("%+v", sv)
	}
	// 1) solo quitar del registro: el contenedor y el volumen siguen
	if err := a.DeleteServer(sv.ID, false, true); err != nil { // removeData sin removeContainer no debe borrar nada
		t.Fatal(err)
	}
	if !testutil.ContainerExists(cname) || !testutil.VolumeExists(vol) {
		t.Fatal("removeContainer=false no debe tocar Docker")
	}
	if l, _ := a.ListServers(); len(l) != 0 {
		t.Fatal("debía quitarse del registro")
	}
	// 2) se puede volver a importar el mismo contenedor (y detecta el volumen)
	imp, err := a.ImportContainer(ImportContainerRequest{ContainerName: cname, AdminPassword: pw})
	if err != nil {
		t.Fatalf("re-importar: %v", err)
	}
	if imp.DataMode != "volume" || imp.DataPath != vol {
		t.Errorf("modo/ruta de datos importados: %s %s (esperaba volume %s)", imp.DataMode, imp.DataPath, vol)
	}
	// 3) eliminar contenedor conservando los datos
	if err := a.DeleteServer(imp.ID, true, false); err != nil {
		t.Fatal(err)
	}
	if testutil.ContainerExists(cname) {
		t.Error("el contenedor debía eliminarse")
	}
	if !testutil.VolumeExists(vol) {
		t.Error("removeData=false debe conservar el volumen")
	}
	// 4) un servicio nuevo con el mismo nombre reutiliza el volumen conservado (aviso: contraseña antigua)
	// eliminar el volumen a mano y comprobar que removeData=true elimina contenedor+volumen en una sola llamada
	testutil.RemoveVolume(vol)
	name2, cname2, vol2 := localNames(t, "dw")
	sv2, err := a.CreateLocalService(CreateServiceRequest{Name: name2, Engine: "postgres", AdminPassword: pw})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteServer(sv2.ID, true, true); err != nil {
		t.Fatal(err)
	}
	if testutil.ContainerExists(cname2) || testutil.VolumeExists(vol2) {
		t.Error("removeContainer+removeData debía eliminar contenedor y volumen")
	}
}

func bindBase() string {
	if b := os.Getenv("DBMANAGER_TEST_BIND_BASE"); b != "" {
		return b
	}
	return os.TempDir()
}

// Modo bind: DeleteServer NUNCA debe borrar la carpeta del host (ni con removeData=true).
// Se usa un contenedor plano con bind (montaje configurado como en el contenedor real del usuario) importado en la app.
func TestDeleteServerBindModeNeverDeletesHostFolder(t *testing.T) {
	dir, err := os.MkdirTemp(bindBase(), "dbm-bind-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	name := testutil.RandName("bindimp")
	c, err := testutil.StartPlain(testutil.PlainOptions{Name: name, Engine: "mssql", Password: testutil.MSSQLPassword, Bind: dir})
	t.Cleanup(func() { testutil.RemoveByName(name) })
	if err != nil {
		t.Fatalf("contenedor con bind: %v", err)
	}
	a, _ := newTestApp(t)
	imp, err := a.ListImportableContainers()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ci := range imp {
		if ci.Name == name {
			found = true
			if ci.DataMode != "bind" || !strings.EqualFold(filepath.Clean(ci.DataPath), filepath.Clean(dir)) {
				t.Errorf("modo/ruta de datos detectados: %s %s (esperaba bind %s)", ci.DataMode, ci.DataPath, dir)
			}
		}
	}
	if !found {
		t.Fatal("el contenedor con bind no se listó")
	}
	sv, err := a.ImportContainer(ImportContainerRequest{ContainerName: name, AdminPassword: c.Password})
	if err != nil {
		t.Fatal(err)
	}
	if sv.DataMode != "bind" {
		t.Fatalf("%+v", sv)
	}
	before, _ := os.ReadDir(dir)
	if len(before) == 0 {
		t.Fatal("el motor no escribió en la carpeta del host")
	}
	// removeData=true con bind: elimina el contenedor pero NUNCA la carpeta del host
	if err := a.DeleteServer(sv.ID, true, true); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	if testutil.ContainerExists(name) {
		t.Error("el contenedor debía eliminarse")
	}
	after, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("la carpeta del host fue eliminada: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("el contenido de la carpeta del host cambió: %d -> %d", len(before), len(after))
	}
}

// BUG (alta en Windows/macOS con Docker Desktop): CreateLocal en modo bind monta la carpeta del host en /var/opt/mssql
// (todo el árbol), y SQL Server no arranca ahí (se queda sin escribir logs; la app agota 150 s con "EOF"). El contenedor
// real del usuario monta SOLO /var/opt/mssql/data. Con ese punto de montaje arranca bien (verificado a mano).
func TestMSSQLBindModeCreateFailsOnDockerDesktop(t *testing.T) {
	dir, err := os.MkdirTemp(bindBase(), "dbm-bind2-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	a, _ := newTestApp(t)
	name, cname, _ := localNames(t, "bm")
	sv, err := a.CreateLocalService(CreateServiceRequest{Name: name, Engine: "mssql", AdminPassword: "Bd!Passw0rd-9", DataMode: "bind", DataPath: dir, MemoryMB: 2048})
	if err != nil {
		t.Fatalf("crear mssql en modo bind: %v", err)
	}
	if err := a.DeleteServer(sv.ID, true, true); err != nil {
		t.Error(err)
	}
	if testutil.ContainerExists(cname) {
		t.Error("contenedor sin eliminar")
	}
}

// BUG (media): dos creaciones simultáneas con puerto automático eligen el mismo puerto (PickPort no reserva).
func TestConcurrentCreateLocalPicksSamePort(t *testing.T) {
	a, _ := newTestApp(t)
	n1, _, _ := localNames(t, "ra")
	n2, _, _ := localNames(t, "rb")
	var wg sync.WaitGroup
	var s1, s2 Server
	var e1, e2 error
	wg.Add(2)
	go func() {
		defer wg.Done()
		s1, e1 = a.CreateLocalService(CreateServiceRequest{Name: n1, Engine: "postgres", AdminPassword: "Rc!Passw0rd-1"})
	}()
	go func() {
		defer wg.Done()
		s2, e2 = a.CreateLocalService(CreateServiceRequest{Name: n2, Engine: "postgres", AdminPassword: "Rc!Passw0rd-2"})
	}()
	wg.Wait()
	if e1 != nil || e2 != nil {
		t.Errorf("creaciones simultáneas con puerto automático: e1=%v e2=%v (puertos %d/%d)", e1, e2, s1.Port, s2.Port)
	}
	if e1 == nil && e2 == nil && s1.Port == s2.Port {
		t.Errorf("ambos servicios usan el mismo puerto %d", s1.Port)
	}
}

// Un contenedor con el mismo nombre ya existente en Docker: error claro y no se toca.
func TestCreateLocalNameCollisionWithExistingContainer(t *testing.T) {
	c := sharedContainer(t, "postgres")
	a, _ := newTestApp(t)
	name := strings.TrimPrefix(c.Name, "dbmanager-")
	_, err := a.CreateLocalService(CreateServiceRequest{Name: name, Engine: "postgres", AdminPassword: "Cc!Passw0rd-1"})
	if err == nil || !strings.Contains(err.Error(), "ya existe un contenedor") {
		t.Fatalf("colisión de nombre de contenedor: %v", err)
	}
	if !testutil.ContainerExists(c.Name) {
		t.Fatal("el contenedor existente desapareció")
	}
	if st := containerState(t, c.Name); st != "running" {
		t.Fatalf("estado del contenedor existente: %s", st)
	}
}

// Validaciones de CreateLocalService que se rechazan antes de tocar Docker, vistas desde la app.
func TestCreateLocalServiceValidationFromApp(t *testing.T) {
	a, _ := newTestApp(t)
	before := testutil.ListNames("dbmanager-")
	for name, req := range map[string]CreateServiceRequest{
		"mssql contraseña débil": {Name: "test-v1", Engine: "mssql", AdminPassword: "weak"},
		"motor":                  {Name: "test-v2", Engine: "mysql", AdminPassword: "x"},
		"bind relativo":          {Name: "test-v3", Engine: "postgres", AdminPassword: "x", DataMode: "bind", DataPath: "rel"},
		"mssql memoria":          {Name: "test-v4", Engine: "mssql", AdminPassword: "Str0ng!Pass1", MemoryMB: 100},
		"nombre largo":           {Name: strings.Repeat("n", 65), Engine: "postgres", AdminPassword: "x"},
	} {
		if _, err := a.CreateLocalService(req); err == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
	after := testutil.ListNames("dbmanager-")
	if len(after) != len(before) {
		t.Errorf("se crearon contenedores: %v -> %v", before, after)
	}
	if l, _ := a.ListServers(); len(l) != 0 {
		t.Error("no debe registrarse nada")
	}
}

// Verifica que CheckEnvironment y la resolución del contexto de Docker funcionan en esta máquina.
func TestCheckEnvironmentAndDockerContext(t *testing.T) {
	a, _ := newTestApp(t)
	env, err := a.CheckEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if !env.DockerAvailable || env.DockerVersion == "" || env.OS == "" || env.Arch == "" {
		t.Fatalf("%+v", env)
	}
	if env.Messages == nil {
		t.Error("Messages no debe ser null")
	}
}
