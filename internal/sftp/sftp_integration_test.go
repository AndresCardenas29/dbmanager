//go:build integration

package sftp

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/store"
	"dbmanager/internal/testutil"

	psftp "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func newMgr(t *testing.T) *Manager {
	t.Helper()
	st, err := store.OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cli, err := dockerx.New()
	if err != nil {
		t.Fatal(err)
	}
	return &Manager{Store: st, Docker: func() (*dockerx.Client, error) { return cli, nil }, BaseDir: filepath.Join(t.TempDir(), "sftp"),
		Emit: func(ev string, id int64, rid string, pct int, msg string, done, isErr bool) {
			t.Logf("%s %3d%% %s", ev, pct, msg)
		}}
}

func upload(t *testing.T, host string, port int, user string, auth ssh.AuthMethod, name, content string) {
	t.Helper()
	c, err := ssh.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)), &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{auth},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("ssh %s: %v", user, err)
	}
	defer c.Close()
	cl, err := psftp.NewClient(c)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	f, err := cl.Create("upload/" + name)
	if err != nil {
		t.Fatalf("crear en upload/: %v", err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	r, err := cl.Open("upload/" + name)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != content {
		t.Fatalf("contenido: %q", b)
	}
}

func runSftpFlow(t *testing.T, mode string) {
	m := newMgr(t)
	ctx := context.Background()
	name := strings.TrimPrefix(testutil.RandName("sftp"), "dbmanager-")
	cname := "dbmanager-" + slug(name)
	data := ""
	if mode == "bind" {
		d, err := os.MkdirTemp(os.TempDir(), "dbm-sftp-")
		if err != nil {
			t.Fatal(err)
		}
		data = d
		t.Cleanup(func() { os.RemoveAll(d) })
	}
	t.Cleanup(func() {
		testutil.RemoveByName(cname)
		if mode == "volume" {
			for _, u := range []string{"alice", "bob"} {
				testutil.RemoveVolume("dbmanager-" + slug(name) + "-data-" + u)
			}
		}
	})
	svc, err := m.Create(ctx, CreateRequest{Name: name, DataMode: mode, DataPath: data})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, fp0, err := m.HostKeys(svc.ID)
	if err != nil {
		t.Fatal(err)
	}

	keyDir := t.TempDir()
	res, err := m.CreateUser(ctx, svc.ID, UserRequest{Name: "alice", Password: "Alice!Passw0rd",
		Key: &KeyRequest{Mode: "generate", KeyType: "ed25519", OutputDir: keyDir, ExportPpk: true}})
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	if res.PrivateKeyPath == "" || res.PpkPath == "" {
		t.Fatalf("rutas: %+v", res)
	}
	if st, _ := os.Stat(res.PrivateKeyPath); st == nil || (os.PathSeparator == '/' && st.Mode().Perm() != 0o600) {
		t.Errorf("permisos de la llave privada: %v", st)
	}
	if _, err := m.CreateUser(ctx, svc.ID, UserRequest{Name: "bob", Password: "Bob!Passw0rd1"}); err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}

	host := "localhost"
	for _, c := range []struct {
		name string
		r    LoginRequest
		ok   bool
	}{
		{"alice llave", LoginRequest{User: "alice", PrivateKeyPath: res.PrivateKeyPath}, true},
		{"alice contraseña", LoginRequest{User: "alice", Password: "Alice!Passw0rd"}, true},
		{"bob contraseña", LoginRequest{User: "bob", Password: "Bob!Passw0rd1"}, true},
		{"bob contraseña mala", LoginRequest{User: "bob", Password: "mala"}, false},
		{"alice sin credenciales válidas", LoginRequest{User: "alice", Password: "x"}, false},
	} {
		c.r.Host, c.r.Port = host, svc.Port
		_, fps, _ := m.HostKeys(svc.ID)
		for _, f := range fps {
			c.r.KnownFingerprints = append(c.r.KnownFingerprints, f[strings.Index(f, " ")+1:])
		}
		r := TestLogin(c.r)
		if r.OK != c.ok {
			t.Errorf("%s: ok=%v mensaje=%q", c.name, r.OK, r.Message)
		}
		if c.ok {
			found := false
			for _, e := range r.Entries {
				if e == "upload/" {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: entradas %v (se esperaba upload/)", c.name, r.Entries)
			}
		}
	}

	// subir un archivo con cada tipo de credencial
	s, _ := LoadSigner(res.PrivateKeyPath, "")
	upload(t, host, svc.Port, "alice", ssh.PublicKeys(s), "hola.txt", "hola desde llave")
	upload(t, host, svc.Port, "bob", ssh.Password("Bob!Passw0rd1"), "bob.txt", "hola bob")

	// la huella del servidor es estable tras recrear (alta de usuario y quitar contraseña)
	if err := m.SetPassword(ctx, svc.ID, "alice", ""); err != nil {
		t.Fatalf("SetPassword vacío con llave: %v", err)
	}
	if r := TestLogin(LoginRequest{Host: host, Port: svc.Port, User: "alice", Password: "Alice!Passw0rd"}); r.OK {
		t.Error("tras quitar la contraseña, alice no debe entrar con contraseña")
	}
	if r := TestLogin(LoginRequest{Host: host, Port: svc.Port, User: "alice", PrivateKeyPath: res.PrivateKeyPath}); !r.OK {
		t.Errorf("alice con llave tras recrear: %s", r.Message)
	}
	_, fp1, _ := m.HostKeys(svc.ID)
	if strings.Join(fp0, ",") != strings.Join(fp1, ",") {
		t.Fatalf("huellas guardadas cambiaron: %v -> %v", fp0, fp1)
	}
	live, err := ScanHostKeys(host, svc.Port)
	if err != nil {
		t.Fatal(err)
	}
	okLive := false
	for _, k := range live {
		for _, f := range fp0 {
			if strings.HasSuffix(f, ssh.FingerprintSHA256(k)) {
				okLive = true
			}
		}
	}
	if !okLive {
		t.Errorf("la huella que ofrece el contenedor no coincide con la persistida (%v)", fp0)
	}
	// los archivos subidos persisten tras la recreación
	if r := TestLogin(LoginRequest{Host: host, Port: svc.Port, User: "bob", Password: "Bob!Passw0rd1"}); !r.OK {
		t.Errorf("bob tras recrear: %s", r.Message)
	}
	c2, err := ssh.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(svc.Port)), &ssh.ClientConfig{User: "bob", Auth: []ssh.AuthMethod{ssh.Password("Bob!Passw0rd1")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	cl, _ := psftp.NewClient(c2)
	if b, err := cl.Open("upload/bob.txt"); err != nil {
		t.Errorf("el archivo no persistió tras recrear: %v", err)
	} else {
		b.Close()
	}
	cl.Close()
	c2.Close()

	// protecciones: sin llaves ni contraseña no se puede dejar al usuario sin acceso
	if err := m.SetPassword(ctx, svc.ID, "bob", ""); err == nil {
		t.Error("quitar la contraseña a un usuario sin llaves debe rechazarse")
	}
	us, _ := m.ListUsers(svc.ID)
	var aliceKey int64
	for _, u := range us {
		if u.User.Name == "alice" {
			aliceKey = u.Keys[0].ID
		}
	}
	if err := m.RemoveKey(ctx, svc.ID, "alice", aliceKey); err == nil {
		t.Error("quitar la última llave sin contraseña debe rechazarse")
	}
	// uids estables
	if us[0].User.UID != 1001 || us[1].User.UID != 1002 {
		t.Errorf("uids: %d %d", us[0].User.UID, us[1].User.UID)
	}

	if err := m.DeleteUser(ctx, svc.ID, "bob", true); err != nil {
		t.Fatal(err)
	}
	if r := TestLogin(LoginRequest{Host: host, Port: svc.Port, User: "bob", Password: "Bob!Passw0rd1"}); r.OK {
		t.Error("bob eliminado no debe entrar")
	}
	if err := m.Delete(ctx, svc.ID, true, true); err != nil {
		t.Fatal(err)
	}
	if testutil.ContainerExists(cname) {
		t.Error("el contenedor no se eliminó")
	}
	if _, err := os.Stat(m.baseOf(svc.ID)); err == nil {
		t.Error("la configuración no se eliminó")
	}
	if mode == "bind" {
		if _, err := os.Stat(data); err != nil {
			t.Error("la carpeta del host (bind) NO debe borrarse")
		}
	}
	fmt.Print()
}

func TestSftpServiceVolumeMode(t *testing.T) { runSftpFlow(t, "volume") }
func TestSftpServiceBindMode(t *testing.T)   { runSftpFlow(t, "bind") }

// Importar un contenedor SFTP existente (modo external): solo ciclo de vida; sus usuarios no se editan y nunca se recrea.
func TestSftpImportExternal(t *testing.T) {
	m := newMgr(t)
	ctx := context.Background()
	name := strings.TrimPrefix(testutil.RandName("sftpx"), "dbmanager-")
	cname := "dbmanager-" + slug(name)
	t.Cleanup(func() {
		testutil.RemoveByName(cname)
		testutil.RemoveVolume("dbmanager-" + slug(name) + "-data-carol")
	})
	svc, err := m.Create(ctx, CreateRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateUser(ctx, svc.ID, UserRequest{Name: "carol", Password: "Carol!Passw0rd"}); err != nil {
		t.Fatal(err)
	}
	// otra instancia de la app (otro store) ve el contenedor como importable
	m2 := newMgr(t)
	list, err := m2.ListImportable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.Name == "sftp-iflow" || c.Name == "sftp_server" {
			t.Logf("importable real detectado (solo lectura): %+v", c)
		}
	}
	found := false
	for _, c := range list {
		if c.Name == cname {
			found = true
		}
	}
	if found {
		t.Fatal("un contenedor creado por la app (label dbmanager.engine=sftp) no debe listarse como importable")
	}
	ext, err := m2.Import(ctx, cname) // la importación directa por nombre sí funciona
	if err != nil {
		t.Fatal(err)
	}
	if ext.Mode != "external" || ext.Port != svc.Port {
		t.Fatalf("%+v", ext)
	}
	if _, err := m2.CreateUser(ctx, ext.ID, UserRequest{Name: "dave", Password: "Dave!Passw0rd1"}); err == nil {
		t.Error("los usuarios de un servicio importado no deben editarse")
	}
	if us, err := m2.ListUsers(ext.ID); err != nil || len(us) != 0 {
		t.Errorf("%v %v", us, err)
	}
	if _, fps, err := m2.HostKeys(ext.ID); err != nil || len(fps) == 0 {
		t.Errorf("huellas por sondeo: %v %v", fps, err)
	}
	if r := TestLogin(LoginRequest{Host: "localhost", Port: ext.Port, User: "carol", Password: "Carol!Passw0rd"}); !r.OK {
		t.Errorf("login en external: %s", r.Message)
	}
	// eliminar un servicio importado solo quita el registro
	if err := m2.Delete(ctx, ext.ID, true, true); err != nil {
		t.Fatal(err)
	}
	if !testutil.ContainerExists(cname) {
		t.Fatal("eliminar un servicio importado NO debe borrar su contenedor")
	}
	if err := m.Delete(ctx, svc.ID, true, true); err != nil {
		t.Fatal(err)
	}
}
