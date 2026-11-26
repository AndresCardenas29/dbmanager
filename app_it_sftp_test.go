//go:build integration

package main

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dbmanager/internal/sftp"
	"dbmanager/internal/testutil"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	psftp "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const osSftp = `C:\Windows\System32\OpenSSH\sftp.exe`

// newSftpSvc crea un servicio SFTP gestionado (nombre test-sf-<hex>) y registra su limpieza total.
func newSftpSvc(t *testing.T, a *App, tag string, req CreateSftpRequest) SftpService {
	t.Helper()
	req.Name = "test-" + tag + "-" + uniq("x")[2:]
	cname := "dbmanager-" + req.Name
	t.Cleanup(func() {
		testutil.RemoveByName(cname)
		testutil.RemoveVolumesPrefix(cname)
	})
	svc, err := a.CreateSftpService(req)
	if err != nil {
		t.Fatalf("CreateSftpService: %v", err)
	}
	return svc
}

func fingerprints(t *testing.T, a *App, id int64) map[string]bool {
	t.Helper()
	fps, err := a.GetSftpHostFingerprints(id)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, f := range fps {
		m[strings.TrimSpace(f[strings.Index(f, " ")+1:])] = true
	}
	return m
}

// goClient abre una sesión SFTP verificando la huella del servidor. OpenSSH 9.8+ penaliza (corta con EOF) las
// conexiones desde una IP tras fallos de autenticación: los errores transitorios se reintentan hasta obtener un
// veredicto definitivo (sesión abierta o rechazo de autenticación).
func goClient(t *testing.T, port int, user string, fps map[string]bool, auth ...ssh.AuthMethod) (*psftp.Client, func(), error) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for {
		c, err := ssh.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), &ssh.ClientConfig{User: user, Auth: auth, Timeout: 10 * time.Second,
			HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
				if !fps[ssh.FingerprintSHA256(k)] {
					return io.ErrUnexpectedEOF
				}
				return nil
			}})
		if err == nil {
			cl, err := psftp.NewClient(c)
			if err != nil {
				c.Close()
				return nil, nil, err
			}
			return cl, func() { cl.Close(); c.Close() }, nil
		}
		m := err.Error()
		if strings.Contains(m, "unable to authenticate") || strings.Contains(m, "no supported methods") || time.Now().After(deadline) {
			return nil, nil, err
		}
		time.Sleep(3 * time.Second)
	}
}

func keyAuth(t *testing.T, path, pass string) ssh.AuthMethod {
	t.Helper()
	s, err := sftp.LoadSigner(path, pass)
	if err != nil {
		t.Fatalf("LoadSigner(%s): %v", path, err)
	}
	return ssh.PublicKeys(s)
}

func readAll(t *testing.T, cl *psftp.Client, p string) (string, error) {
	f, err := cl.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	return string(b), err
}

func putFile(cl *psftp.Client, p, content string) error {
	f, err := cl.Create(p)
	if err != nil {
		return err
	}
	_, err = f.Write([]byte(content))
	f.Close()
	return err
}

// runOSSftp usa el cliente OpenSSH del sistema en modo batch con known_hosts construido por la app.
func runOSSftp(t *testing.T, a *App, svc SftpService, user, key, batch string) (string, error) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("ruta de sftp.exe solo en Windows")
	}
	if _, err := os.Stat(osSftp); err != nil {
		t.Skip("sin cliente OpenSSH")
	}
	kh, err := a.GetSftpHostKeys(svc.ID)
	if err != nil || len(kh) == 0 {
		t.Fatalf("GetSftpHostKeys: %v %v", kh, err)
	}
	dir := t.TempDir()
	khf := filepath.Join(dir, "known_hosts")
	os.WriteFile(khf, []byte(strings.Join(kh, "\n")+"\n"), 0o600)
	bf := filepath.Join(dir, "batch.txt")
	os.WriteFile(bf, []byte(batch), 0o600)
	args := []string{"-4", "-P", strconv.Itoa(svc.Port), "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + khf,
		"-o", "PasswordAuthentication=no", "-o", "ConnectTimeout=10", "-b", bf}
	if key != "" {
		args = append(args, "-i", key, "-o", "IdentitiesOnly=yes")
	}
	args = append(args, user+"@localhost")
	cmd := exec.Command(osSftp, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// fixACL devuelve una copia de la llave con ACL restringida (lo que OpenSSH de Windows exige).
func fixACL(t *testing.T, key string) string {
	t.Helper()
	cp := filepath.Join(t.TempDir(), filepath.Base(key))
	b, _ := os.ReadFile(key)
	os.WriteFile(cp, b, 0o600)
	user := os.Getenv("USERDOMAIN") + `\` + os.Getenv("USERNAME")
	exec.Command("icacls", cp, "/inheritance:r").Run()
	exec.Command("icacls", cp, "/grant:r", user+":(R)").Run()
	return cp
}

func newSftpApp(t *testing.T) (*App, *evLog) {
	a, _ := newTestApp(t)
	evl := &evLog{}
	a.emit = func(event string, data any) {
		if p, ok := data.(ProgressEvent); ok {
			evl.add(event, servicesProgress(p))
		}
	}
	return a, evl
}

// ------------------------------------------------------------------------------------------------

func TestSftpManagedServiceFullFlow(t *testing.T) {
	a, evl := newSftpApp(t)
	outDir := t.TempDir()
	svc := newSftpSvc(t, a, "sf", CreateSftpRequest{RequestID: "sf-1"})
	cname := svc.ContainerName
	if svc.Mode != "managed" || svc.Port < 2224 || svc.DataMode != "volume" || svc.ExposeOnNetwork {
		t.Errorf("servicio: %+v", svc)
	}
	evs := evl.of("sftp:progress")
	if len(evs) < 3 {
		t.Errorf("pocos eventos: %d", len(evs))
	}
	for i, e := range evs {
		if e.RequestID != "sf-1" {
			t.Errorf("evento %d sin requestId: %+v", i, e)
		}
	}
	if last := evs[len(evs)-1]; !last.Done || last.Error || last.ID != svc.ID {
		t.Errorf("último evento: %+v", last)
	}
	// escucha solo en 127.0.0.1 por defecto
	cli := testutil.Docker(t)
	defer cli.Close()
	insp, err := cli.Raw().ContainerInspect(context.Background(), cname)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range insp.NetworkSettings.Ports[nat.Port("22/tcp")] {
		if b.HostIP != "127.0.0.1" {
			t.Errorf("el puerto SFTP escucha en %s (esperaba 127.0.0.1 sin exposeOnNetwork)", b.HostIP)
		}
	}
	fp0 := fingerprints(t, a, svc.ID)
	if len(fp0) < 2 {
		t.Fatalf("huellas: %v", fp0)
	}

	// ---- usuarios
	const alicePW = "Al1ce: pw/con\"raro'$x"
	ar, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "alice", Password: alicePW,
		Key: &SftpKeyRequest{Mode: "generate", KeyType: "ed25519", Passphrase: "frase alice", OutputDir: outDir, ExportPpk: true}})
	if err != nil {
		t.Fatalf("CreateSftpUser alice: %v", err)
	}
	if ar.PrivateKeyPath == "" || ar.PublicKeyPath == "" || ar.PpkPath == "" || len(ar.User.Keys) != 1 || !ar.User.HasPassword || ar.User.UploadDir != "upload" {
		t.Errorf("resultado alice: %+v", ar)
	}
	noSecret(t, "SftpUserResult", mustJSON(t, ar), alicePW, "frase alice")
	// llaves existentes jamás se sobrescriben
	pre := filepath.Join(outDir, "bob_id_ed25519")
	os.WriteFile(pre, []byte("LLAVE PREEXISTENTE DEL USUARIO"), 0o600)
	br, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "bob", Key: &SftpKeyRequest{Mode: "generate", KeyType: "ed25519", OutputDir: outDir}})
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	if b, _ := os.ReadFile(pre); string(b) != "LLAVE PREEXISTENTE DEL USUARIO" {
		t.Error("se sobrescribió una llave privada existente")
	}
	if br.PrivateKeyPath == pre || !strings.Contains(br.PrivateKeyPath, "bob_id_ed25519_2") {
		t.Errorf("ruta de la nueva llave: %s", br.PrivateKeyPath)
	}
	if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "carol", Password: "C4rol-pw-1"}); err != nil {
		t.Fatalf("carol: %v", err)
	}
	users, _ := a.ListSftpUsers(svc.ID)
	uids := map[int]string{}
	for _, u := range users {
		if prev, dup := uids[u.UID]; dup {
			t.Errorf("uid %d repetido: %s y %s", u.UID, prev, u.Name)
		}
		uids[u.UID] = u.Name
		if u.UID < 1001 {
			t.Errorf("uid %d fuera de rango", u.UID)
		}
	}
	// huellas estables tras recrear el contenedor tres veces
	if fp := fingerprints(t, a, svc.ID); len(fp) != len(fp0) {
		t.Errorf("huellas cambiaron: %v vs %v", fp, fp0)
	}
	fps := fingerprints(t, a, svc.ID)

	// ---- users.conf y docker inspect: sin contraseñas en claro
	uc, err := os.ReadFile(filepath.Join(a.store.Dir(), "sftp", strconv.FormatInt(svc.ID, 10), "users.conf"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("users.conf:\n%s", uc)
	for _, secret := range []string{alicePW, "C4rol-pw-1", "frase alice"} {
		if strings.Contains(string(uc), secret) {
			t.Errorf("users.conf contiene una contraseña en claro: %q", secret)
		}
	}
	if !strings.Contains(string(uc), "alice:$6$") || !strings.Contains(string(uc), "bob::") {
		t.Errorf("formato inesperado de users.conf")
	}
	insp, _ = cli.Raw().ContainerInspect(context.Background(), cname)
	envdump := strings.Join(insp.Config.Env, "\n") + strings.Join(insp.Config.Cmd, " ") + strings.Join(insp.Config.Entrypoint, " ")
	for _, secret := range []string{alicePW, "C4rol-pw-1", "frase alice"} {
		if strings.Contains(envdump, secret) {
			t.Errorf("docker inspect expone una contraseña: %q", secret)
		}
	}
	// permisos de las llaves privadas
	for _, p := range []string{ar.PrivateKeyPath, ar.PpkPath, br.PrivateKeyPath} {
		if st, err := os.Stat(p); err != nil {
			t.Error(err)
		} else if runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s con permisos %v", p, st.Mode().Perm())
		}
	}

	// ---- logins reales (cliente Go y TestSftpLogin de la app)
	res, _ := a.TestSftpLogin(SftpLoginTest{ServiceID: svc.ID, User: "alice", PrivateKeyPath: ar.PrivateKeyPath, Passphrase: "frase alice"})
	if !res.OK || !contains(res.Entries, "upload/") || res.HostFingerprint == "" {
		t.Errorf("login de alice con llave: %+v", res)
	}
	if res, _ = a.TestSftpLogin(SftpLoginTest{ServiceID: svc.ID, User: "alice", Password: alicePW}); !res.OK {
		t.Errorf("login de alice con contraseña (caracteres raros): %+v", res)
	}
	if res, _ = a.TestSftpLogin(SftpLoginTest{ServiceID: svc.ID, User: "alice", PrivateKeyPath: ar.PrivateKeyPath, Passphrase: "mala"}); res.OK || strings.Contains(res.Message, "frase alice") {
		t.Errorf("frase de paso mala: %+v", res)
	}
	if res, _ = a.TestSftpLogin(SftpLoginTest{ServiceID: svc.ID, User: "alice", Password: "incorrecta"}); res.OK {
		t.Error("contraseña incorrecta aceptada")
	}
	noSecret(t, "resultado de login", mustJSON(t, res), alicePW)

	// bob: solo llave -> NINGUNA contraseña funciona (ni vacía)
	for _, pw := range []string{"", "x", "bob", "Bob-Pass-1"} {
		_, cl, err := goClient(t, svc.Port, "bob", fps, ssh.Password(pw))
		if err == nil {
			cl()
			t.Errorf("bob (solo llave) entró con la contraseña %q", pw)
		}
	}
	_, cl, err := goClient(t, svc.Port, "bob", fps, ssh.KeyboardInteractive(func(string, string, []string, []bool) ([]string, error) { return []string{""}, nil }))
	if err == nil {
		cl()
		t.Error("bob entró por keyboard-interactive vacío")
	}
	bobKey := keyAuth(t, br.PrivateKeyPath, "")
	bc, closeB, err := goClient(t, svc.Port, "bob", fps, bobKey)
	if err != nil {
		t.Fatalf("bob con llave: %v", err)
	}
	defer closeB()

	// ---- jaula chroot y aislamiento
	ac, closeA, err := goClient(t, svc.Port, "alice", fps, ssh.Password(alicePW))
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	defer closeA()
	if err := putFile(ac, "upload/secreto-alice.txt", "solo alice"); err != nil {
		t.Fatalf("alice sube: %v", err)
	}
	if err := putFile(ac, "raiz-no-escribible.txt", "x"); err == nil {
		t.Error("alice pudo escribir en la raíz de su jaula (debe ser de root)")
	}
	if _, err := readAll(t, ac, "/etc/passwd"); err == nil {
		t.Error("ESCAPE: alice leyó /etc/passwd")
	}
	if _, err := readAll(t, ac, "../../etc/passwd"); err == nil {
		t.Error("ESCAPE: alice leyó ../../etc/passwd")
	}
	root, _ := ac.ReadDir("/")
	for _, e := range root {
		if e.Name() != "upload" && e.Name() != ".ssh" {
			t.Errorf("la raíz de alice contiene %q (solo debe ver 'upload' y .ssh)", e.Name())
		}
	}
	up, _ := ac.ReadDir("/..")
	if len(up) != len(root) {
		t.Errorf("'/..' lista otra cosa que la raíz de la jaula: %d vs %d", len(up), len(root))
	}
	for _, p := range []string{"/home/alice", "/home/bob", "/home", "/etc", "/proc/self/environ"} {
		if es, err := ac.ReadDir(p); err == nil && len(es) > 0 {
			t.Errorf("alice puede listar %s: %d entradas", p, len(es))
		}
	}
	// bob no ve lo de alice
	for _, p := range []string{"/home/alice/upload/secreto-alice.txt", "/upload/secreto-alice.txt", "/../alice/upload/secreto-alice.txt"} {
		if _, err := readAll(t, bc, p); err == nil {
			t.Errorf("bob leyó %s", p)
		}
	}
	if es, _ := bc.ReadDir("/upload"); len(es) != 0 {
		t.Errorf("bob ve archivos en su upload: %v", es)
	}
	// symlink hacia fuera: alice puede crear un symlink pero no debe poder seguirlo hacia el host
	if err := ac.Symlink("/etc/passwd", "upload/enlace"); err == nil {
		if _, err := readAll(t, ac, "upload/enlace"); err == nil {
			t.Error("ESCAPE: symlink a /etc/passwd legible desde la jaula")
		}
	}

	// ---- cliente OpenSSH del sistema (batch) con known_hosts de la app
	keyForOS := ar.PrivateKeyPath
	out, err := runOSSftp(t, a, svc, "bob", br.PrivateKeyPath, "ls\n")
	if err != nil && (strings.Contains(out, "UNPROTECTED") || strings.Contains(out, "bad permissions")) {
		t.Logf("BUG: OpenSSH de Windows rechaza la llave generada por la app por ACL demasiado abierta:\n%s", out)
		t.Errorf("la llave privada generada no es utilizable por el sftp.exe del sistema (ACL heredada demasiado permisiva)")
		fixed := fixACL(t, br.PrivateKeyPath)
		out, err = runOSSftp(t, a, svc, "bob", fixed, "ls\n")
		keyForOS = fixACL(t, ar.PrivateKeyPath)
	}
	if err != nil || !strings.Contains(out, "upload") {
		t.Errorf("sftp.exe como bob: err=%v\n%s", err, out)
	}
	out, err = runOSSftp(t, a, svc, "bob", fixACL(t, br.PrivateKeyPath), "cd ..\npwd\ncd /etc\nls /\nls /home\nget /etc/passwd "+filepath.Join(t.TempDir(), "p")+"\n")
	t.Logf("sftp.exe intento de escape:\n%s", out)
	if strings.Contains(out, "root:") || strings.Contains(out, "bin/sh") {
		t.Error("ESCAPE por sftp.exe")
	}
	if err == nil {
		t.Errorf("sftp.exe: los comandos de escape debían producir errores (cd /etc, get /etc/passwd)")
	}
	// huella desconocida: StrictHostKeyChecking debe rechazar con known_hosts distinto
	if runtime.GOOS == "windows" {
		kh := filepath.Join(t.TempDir(), "kh")
		os.WriteFile(kh, []byte("[localhost]:"+strconv.Itoa(svc.Port)+" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHoZsLQcA0m5x5s6fU3o6mYb1c8yq7sQb4n1e3p0k2aB\n"), 0o600)
		bf := filepath.Join(t.TempDir(), "b")
		os.WriteFile(bf, []byte("ls\n"), 0o600)
		o, err := exec.Command(osSftp, "-4", "-P", strconv.Itoa(svc.Port), "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile="+kh,
			"-i", fixACL(t, br.PrivateKeyPath), "-b", bf, "bob@localhost").CombinedOutput()
		if err == nil {
			t.Errorf("sftp.exe aceptó un known_hosts con otra huella:\n%s", o)
		}
	}
	_ = keyForOS

	// ---- llaves: agregar / quitar surten efecto tras la recreación
	k2dir := t.TempDir()
	r2, err := a.AddSftpUserKey(svc.ID, "alice", SftpKeyRequest{Mode: "generate", KeyType: "ed25519", OutputDir: k2dir})
	if err != nil || len(r2.User.Keys) != 2 {
		t.Fatalf("AddSftpUserKey: %v %+v", err, r2.User.Keys)
	}
	// el archivo subido por alice sobrevive a la recreación
	ac2, closeA2, err := goClient(t, svc.Port, "alice", fingerprints(t, a, svc.ID), keyAuth(t, r2.PrivateKeyPath, ""))
	if err != nil {
		t.Fatalf("login con la llave recién agregada: %v", err)
	}
	if s, err := readAll(t, ac2, "upload/secreto-alice.txt"); err != nil || s != "solo alice" {
		t.Errorf("el archivo no persistió tras recrear: %q %v", s, err)
	}
	closeA2()
	var firstKeyID int64
	for _, k := range r2.User.Keys {
		if firstKeyID == 0 {
			firstKeyID = k.ID
		}
	}
	if err := a.RemoveSftpUserKey(svc.ID, "alice", firstKeyID); err != nil {
		t.Fatal(err)
	}
	fps = fingerprints(t, a, svc.ID)
	if _, c, err := goClient(t, svc.Port, "alice", fps, keyAuth(t, ar.PrivateKeyPath, "frase alice")); err == nil {
		c()
		t.Error("la llave quitada sigue funcionando tras recrear")
	}
	if _, c, err := goClient(t, svc.Port, "alice", fps, keyAuth(t, r2.PrivateKeyPath, "")); err != nil {
		t.Errorf("la llave restante dejó de funcionar: %v", err)
	} else {
		c()
	}
	// quitar la última llave de un usuario sin contraseña: rechazado; quitar la contraseña sin llaves: rechazado
	bobU, _ := a.ListSftpUsers(svc.ID)
	var bobKeyID int64
	for _, u := range bobU {
		if u.Name == "bob" {
			bobKeyID = u.Keys[0].ID
		}
	}
	if err := a.RemoveSftpUserKey(svc.ID, "bob", bobKeyID); err == nil {
		t.Error("quitar la última llave de un usuario sin contraseña debía rechazarse")
	}
	if err := a.SetSftpUserPassword(svc.ID, "carol", ""); err == nil {
		t.Error("quitar la contraseña de un usuario sin llaves debía rechazarse")
	}
	if err := a.RemoveSftpUserKey(svc.ID, "bob", 999999); err == nil {
		t.Error("llave inexistente debía fallar")
	}
	// contraseña: cambiar, y luego dar contraseña a bob; quitarla de alice (tiene llave)
	if err := a.SetSftpUserPassword(svc.ID, "carol", "N3w-carol-pw"); err != nil {
		t.Fatal(err)
	}
	fps = fingerprints(t, a, svc.ID)
	if _, c, err := goClient(t, svc.Port, "carol", fps, ssh.Password("C4rol-pw-1")); err == nil {
		c()
		t.Error("la contraseña anterior de carol sigue valiendo")
	}
	if _, c, err := goClient(t, svc.Port, "carol", fps, ssh.Password("N3w-carol-pw")); err != nil {
		t.Errorf("la nueva contraseña de carol no funciona: %v", err)
	} else {
		c()
	}
	if err := a.SetSftpUserPassword(svc.ID, "alice", ""); err != nil {
		t.Fatal(err)
	}
	if _, c, err := goClient(t, svc.Port, "alice", fingerprints(t, a, svc.ID), ssh.Password(alicePW)); err == nil {
		c()
		t.Error("alice entró con contraseña tras quitársela")
	}

	// ---- ciclo de vida
	if err := a.StopSftpService(svc.ID); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.SftpServiceStatus(svc.ID); s.State != "stopped" {
		t.Errorf("estado: %+v", s)
	}
	if r, _ := a.TestSftpLogin(SftpLoginTest{ServiceID: svc.ID, User: "carol", Password: "N3w-carol-pw"}); r.OK || !testutil.HasSpanish(r.Message) {
		t.Errorf("login con el servicio detenido: %+v", r)
	}
	if err := a.StartSftpService(svc.ID); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, "ssh", func() bool {
		r, _ := a.TestSftpLogin(SftpLoginTest{ServiceID: svc.ID, User: "carol", Password: "N3w-carol-pw"})
		return r.OK
	})
	if err := a.RestartSftpService(svc.ID); err != nil {
		t.Fatal(err)
	}
	if lg, err := a.SftpServiceLogs(svc.ID, 20); err != nil || strings.TrimSpace(lg) == "" {
		t.Errorf("logs: %q %v", lg, err)
	}
	if got := fingerprints(t, a, svc.ID); len(got) != len(fp0) {
		t.Error("huella cambió")
	}
	waitUntil(t, 30*time.Second, "ssh tras reinicio", func() bool {
		r, _ := a.TestSftpLogin(SftpLoginTest{ServiceID: svc.ID, User: "carol", Password: "N3w-carol-pw"})
		return r.OK
	})
	// la huella que ve el login es una de las anunciadas
	r, _ := a.TestSftpLogin(SftpLoginTest{ServiceID: svc.ID, User: "carol", Password: "N3w-carol-pw"})
	if !fps[r.HostFingerprint] {
		t.Errorf("huella del servidor %s no está entre las anunciadas %v", r.HostFingerprint, fps)
	}
	ops, _ := a.ListOperations(100)
	noSecret(t, "operaciones", mustJSON(t, ops), alicePW, "C4rol-pw-1", "N3w-carol-pw", "frase alice")
}

// Usuarios, directorios y llaves pegadas hostiles.
func TestSftpHostileInputs(t *testing.T) {
	a, _ := newSftpApp(t)
	svc := newSftpSvc(t, a, "sh", CreateSftpRequest{})
	g, _ := sftp.Generate("ed25519", "", "ok")
	g2, _ := sftp.Generate("ed25519", "", "ok2")
	badUsers := []string{"root", "sshd", "nobody", "Alice", "a b", "a;b", "../x", "x/y", "-x", "1abc", "", " ", "a\nroot", "ñandú", strings.Repeat("a", 33), "a:b", "a$(id)"}
	for _, n := range badUsers {
		if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: n, Password: "Pw-12345678"}); err == nil {
			t.Errorf("usuario %q debía rechazarse", n)
		} else if !testutil.HasSpanish(err.Error()) {
			t.Errorf("mensaje de %q no está en español: %v", n, err)
		}
	}
	for _, d := range []string{"../etc", "a/b", ".", "..", "a b", "x;y", "/abs", "a\nb", strings.Repeat("a", 65), "-r"} {
		if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "okuser", Password: "Pw-12345678", UploadDir: d}); err == nil {
			t.Errorf("uploadDir %q debía rechazarse", d)
			a.DeleteSftpUser(svc.ID, "okuser", true)
		}
	}
	if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "nopass"}); err == nil {
		t.Error("usuario sin contraseña ni llave debía rechazarse")
	}
	if l, _ := a.ListSftpUsers(svc.ID); len(l) != 0 {
		t.Fatalf("no debe haber usuarios tras entradas inválidas: %+v", l)
	}
	// llaves pegadas
	pasted := map[string]string{
		"con command=":         `command="/bin/sh -c 'cat /etc/passwd'" ` + g.PublicLine,
		"con from=":            `from="10.0.0.1" ` + g.PublicLine,
		"con no-pty":           `no-pty,no-port-forwarding ` + g.PublicLine,
		"dos líneas":           g.PublicLine + "\n" + g2.PublicLine,
		"CRLF dos líneas":      g.PublicLine + "\r\n" + g2.PublicLine,
		"basura":               "ssh-ed25519 NOPE",
		"vacía":                "",
		"privada pegada":       "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----",
		"inyección comentario": g.PublicLine + "\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHoZsLQcA0m5x5s6fU3o6mYb1c8yq7sQb4n1e3p0k2aB evil",
	}
	for n, k := range pasted {
		if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "pk", Key: &SftpKeyRequest{Mode: "paste", PublicKey: k}}); err == nil {
			t.Errorf("llave pegada %q debía rechazarse", n)
			a.DeleteSftpUser(svc.ID, "pk", true)
		}
	}
	if l, _ := a.ListSftpUsers(svc.ID); len(l) != 0 {
		t.Fatalf("quedaron usuarios: %+v", l)
	}
	// llave válida pegada y duplicado
	r, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "pk", Key: &SftpKeyRequest{Mode: "paste", PublicKey: "  " + g.PublicLine + "  "}})
	if err != nil || len(r.User.Keys) != 1 || strings.ContainsAny(r.User.Keys[0].PublicKey, "\r\n") {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := a.AddSftpUserKey(svc.ID, "pk", SftpKeyRequest{Mode: "paste", PublicKey: g.PublicLine}); err == nil {
		t.Error("llave duplicada debía rechazarse")
	}
	if _, err := a.AddSftpUserKey(svc.ID, "pk", SftpKeyRequest{Mode: "x"}); err == nil {
		t.Error("modo inválido debía rechazarse")
	}
	if _, err := a.AddSftpUserKey(svc.ID, "no-existe", SftpKeyRequest{Mode: "paste", PublicKey: g2.PublicLine}); err == nil {
		t.Error("usuario inexistente debía rechazarse")
	}
	// usuario duplicado
	if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "pk", Password: "Pw-12345678"}); err == nil {
		t.Error("usuario duplicado debía rechazarse")
	}
	// el archivo .pub generado para el contenedor tiene exactamente 1 línea
	kf, _ := os.ReadFile(filepath.Join(a.store.Dir(), "sftp", strconv.FormatInt(svc.ID, 10), "keys", "pk", "key1.pub"))
	if n := strings.Count(strings.TrimSpace(string(kf)), "\n"); n != 0 {
		t.Errorf("key1.pub con %d saltos de línea: %q", n, kf)
	}
}

// BUG (baja/media): el comentario de una llave GENERADA no se sanea: un salto de línea en él produce un .pub de varias líneas,
// que se monta como authorized_keys del usuario (permite inyectar una segunda llave con opciones).
func TestGeneratedKeyCommentNewlineInjection(t *testing.T) {
	a, _ := newSftpApp(t)
	svc := newSftpSvc(t, a, "sc", CreateSftpRequest{})
	evil, _ := sftp.Generate("ed25519", "", "evil")
	comment := "ok\n" + `command="cat /etc/passwd" ` + evil.PublicLine
	r, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "inj", Key: &SftpKeyRequest{Mode: "generate", KeyType: "ed25519", Comment: comment, OutputDir: t.TempDir()}})
	if err != nil {
		t.Logf("rechazado (correcto): %v", err)
		return
	}
	kf, _ := os.ReadFile(filepath.Join(a.store.Dir(), "sftp", strconv.FormatInt(svc.ID, 10), "keys", "inj", "key1.pub"))
	if n := strings.Count(strings.TrimSpace(string(kf)), "\n"); n != 0 {
		t.Errorf("la llave generada produjo un .pub de %d líneas (inyección de authorized_keys):\n%s", n+1, kf)
	}
	pub, _ := os.ReadFile(r.PublicKeyPath)
	if strings.Count(strings.TrimSpace(string(pub)), "\n") != 0 {
		t.Errorf("el .pub entregado al usuario es multilínea: %q", pub)
	}
}

// BUG (media, Windows): las llaves privadas se escriben con os.WriteFile(0o600), que en Windows no cambia la ACL: hereda la
// de la carpeta (p. ej. Users: lectura). OpenSSH de Windows las rechaza (UNPROTECTED PRIVATE KEY FILE) y cualquier usuario
// local puede leerlas.
func TestPrivateKeyACLTooOpenOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("solo Windows")
	}
	a, _ := newSftpApp(t)
	svc := newSftpSvc(t, a, "sa", CreateSftpRequest{})
	// carpeta de salida bajo C:\Users\Public (ACL heredada típica), no bajo el perfil restringido
	out, err := os.MkdirTemp(`C:\Users\Public`, "dbm-key-")
	if err != nil {
		t.Skipf("sin C:\\Users\\Public: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(out) })
	r, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "acl", Key: &SftpKeyRequest{Mode: "generate", KeyType: "ed25519", OutputDir: out, ExportPpk: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{r.PrivateKeyPath, r.PpkPath} {
		o, _ := exec.Command("icacls", p).CombinedOutput()
		for _, wide := range []string{"Everyone", `BUILTIN\Users`, "Authenticated Users", `NT AUTHORITY\Authenticated Users`} {
			if strings.Contains(string(o), wide) {
				t.Errorf("%s es legible por %q:\n%s", filepath.Base(p), wide, o)
				break
			}
		}
	}
}

// Colisión de puertos, exposeOnNetwork y creación concurrente.
func TestSftpPortsAndExposure(t *testing.T) {
	a, _ := newSftpApp(t)
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	busy := l.Addr().(*net.TCPAddr).Port
	_, err = a.CreateSftpService(CreateSftpRequest{Name: "test-sp-busy", Port: busy})
	l.Close()
	if err == nil || !strings.Contains(err.Error(), "ya está en uso") {
		t.Errorf("puerto ocupado: %v", err)
	}
	if testutil.ContainerExists("dbmanager-test-sp-busy") {
		t.Error("no debió crearse el contenedor")
	}
	if l, _ := a.ListSftpServices(); len(l) != 0 {
		t.Errorf("no debe registrarse: %+v", l)
	}
	for name, req := range map[string]CreateSftpRequest{
		"nombre vacío": {Name: " "}, "bind relativo": {Name: "test-sp-x", DataMode: "bind", DataPath: "rel"}, "modo": {Name: "test-sp-y", DataMode: "nfs"},
		"puerto": {Name: "test-sp-z", Port: 70000}, "imagen": {Name: "test-sp-w", Image: "a b"},
	} {
		if _, err := a.CreateSftpService(req); err == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
	// dos creaciones simultáneas: puertos distintos, ambas OK
	n1, n2 := "test-sp-a-"+uniq("x")[2:], "test-sp-b-"+uniq("x")[2:]
	for _, n := range []string{n1, n2} {
		cn := "dbmanager-" + n
		t.Cleanup(func() { testutil.RemoveByName(cn); testutil.RemoveVolumesPrefix(cn) })
	}
	var wg sync.WaitGroup
	var s1, s2 SftpService
	var e1, e2 error
	wg.Add(2)
	go func() {
		defer wg.Done()
		s1, e1 = a.CreateSftpService(CreateSftpRequest{Name: n1, ExposeOnNetwork: true})
	}()
	go func() { defer wg.Done(); s2, e2 = a.CreateSftpService(CreateSftpRequest{Name: n2}) }()
	wg.Wait()
	if e1 != nil || e2 != nil {
		t.Fatalf("creaciones simultáneas: %v / %v", e1, e2)
	}
	if s1.Port == s2.Port {
		t.Errorf("mismo puerto %d", s1.Port)
	}
	cli := testutil.Docker(t)
	defer cli.Close()
	for _, c := range []struct {
		s    SftpService
		want string
	}{{s1, "0.0.0.0"}, {s2, "127.0.0.1"}} {
		insp, _ := cli.Raw().ContainerInspect(context.Background(), c.s.ContainerName)
		for _, b := range insp.NetworkSettings.Ports[nat.Port("22/tcp")] {
			if b.HostIP != c.want {
				t.Errorf("%s publica en %s, esperaba %s", c.s.Name, b.HostIP, c.want)
			}
		}
	}
	// nombre duplicado (otra capitalización) y puerto ya usado por otro servicio SFTP
	if _, err := a.CreateSftpService(CreateSftpRequest{Name: strings.ToUpper(s1.Name)}); err == nil {
		t.Error("nombre duplicado debía fallar")
	}
	if _, err := a.CreateSftpService(CreateSftpRequest{Name: "test-sp-dup-" + uniq("x")[2:], Port: s1.Port}); err == nil {
		t.Error("puerto de otro servicio SFTP debía fallar")
	}
	for _, s := range []SftpService{s1, s2} {
		if err := a.DeleteSftpService(s.ID, true, true); err != nil {
			t.Errorf("DeleteSftpService: %v", err)
		}
	}
}

// Eliminar usuarios: deleteFiles false/true, uid y propiedad al recrear, y volúmenes huérfanos al borrar el servicio.
func TestSftpDeleteUserAndData(t *testing.T) {
	a, _ := newSftpApp(t)
	svc := newSftpSvc(t, a, "sd", CreateSftpRequest{})
	prefix := svc.ContainerName + "-data"
	volOf := func(u string) string { return svc.DataPath + "-" + u }
	for _, u := range []string{"dave", "erin"} {
		if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: u, Password: "Pw-" + u + "-12345", Key: pasteKey()}); err != nil {
			t.Fatal(err)
		}
	}
	login := func(u string) (*psftp.Client, func()) {
		var cl *psftp.Client
		var closer func()
		waitUntil(t, 30*time.Second, "login "+u, func() bool {
			var err error
			cl, closer, err = goClient(t, svc.Port, u, fingerprints(t, a, svc.ID), ssh.Password("Pw-"+u+"-12345"))
			return err == nil
		})
		return cl, closer
	}
	dc, cl := login("dave")
	if err := putFile(dc, "upload/viejo.txt", "datos de dave"); err != nil {
		t.Fatal(err)
	}
	cl()
	oldUID := 0
	us, _ := a.ListSftpUsers(svc.ID)
	for _, u := range us {
		if u.Name == "dave" {
			oldUID = u.UID
		}
	}
	// deleteFiles=false conserva el volumen
	if err := a.DeleteSftpUser(svc.ID, "dave", false); err != nil {
		t.Fatal(err)
	}
	if !contains(testutil.VolumesWithPrefix(prefix), volOf("dave")) {
		t.Errorf("deleteFiles=false debía conservar el volumen %s; hay %v", volOf("dave"), testutil.VolumesWithPrefix(prefix))
	}
	// el usuario ya no puede entrar
	if _, c, err := goClient(t, svc.Port, "dave", fingerprints(t, a, svc.ID), ssh.Password("Pw-dave-12345")); err == nil {
		c()
		t.Error("dave sigue pudiendo entrar tras eliminarlo")
	}
	// recrear con el mismo nombre: ¿hereda los archivos? ¿puede modificarlos con su nuevo uid?
	if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "dave", Password: "Pw-dave-12345", Key: pasteKey()}); err != nil {
		t.Fatal(err)
	}
	us, _ = a.ListSftpUsers(svc.ID)
	for _, u := range us {
		if u.Name == "dave" {
			t.Logf("uid de dave: antes %d, ahora %d", oldUID, u.UID)
		}
	}
	if r0, _ := a.TestSftpLogin(SftpLoginTest{ServiceID: svc.ID, User: "dave", Password: "Pw-dave-12345"}); !r0.OK {
		lg, _ := a.SftpServiceLogs(svc.ID, 40)
		t.Logf("login de dave recreado falló: %s / logs: %s", r0.Message, lg)
	}
	dc, cl = login("dave")
	s, rerr := readAll(t, dc, "upload/viejo.txt")
	t.Logf("dave recreado ve el archivo antiguo: %q err=%v", s, rerr)
	if rerr == nil {
		if err := putFile(dc, "upload/viejo.txt", "sobrescrito"); err != nil {
			t.Errorf("el usuario recreado no puede modificar los archivos de su propio volumen (uid distinto: propiedad no actualizada): %v", err)
		}
	}
	cl()
	// deleteFiles=true elimina el volumen
	if err := a.DeleteSftpUser(svc.ID, "erin", true); err != nil {
		t.Fatal(err)
	}
	if contains(testutil.VolumesWithPrefix(prefix), volOf("erin")) {
		t.Error("deleteFiles=true debía eliminar el volumen")
	}
	// usuario eliminado con deleteFiles=false y luego el SERVICIO con removeData=true: el volumen huérfano debe eliminarse
	if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "frank", Password: "Pw-frank-12345", Key: pasteKey()}); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteSftpUser(svc.ID, "frank", false); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteSftpService(svc.ID, true, true); err != nil {
		t.Fatal(err)
	}
	if left := testutil.VolumesWithPrefix(prefix); len(left) != 0 {
		t.Errorf("BUG: volúmenes huérfanos tras DeleteSftpService(removeData=true): %v", left)
	}
	if _, err := os.Stat(filepath.Join(a.store.Dir(), "sftp", strconv.FormatInt(svc.ID, 10))); err == nil {
		t.Error("la carpeta de configuración debía eliminarse")
	}
	if testutil.ContainerExists(svc.ContainerName) {
		t.Error("el contenedor debía eliminarse")
	}
}

// Modo bind: la carpeta del host nunca se borra.
func TestSftpBindModeNeverDeletesHostFolder(t *testing.T) {
	a, _ := newSftpApp(t)
	dir, err := os.MkdirTemp(bindBase(), "dbm-sftp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	svc := newSftpSvc(t, a, "sb", CreateSftpRequest{DataMode: "bind", DataPath: dir})
	if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "gina", Password: "Pw-gina-12345"}); err != nil {
		t.Fatal(err)
	}
	var cl func()
	var c *psftp.Client
	waitUntil(t, 30*time.Second, "login", func() bool {
		var err error
		c, cl, err = goClient(t, svc.Port, "gina", fingerprints(t, a, svc.ID), ssh.Password("Pw-gina-12345"))
		return err == nil
	})
	if err := putFile(c, "upload/hostfile.txt", "en el host"); err != nil {
		t.Fatalf("subida en bind: %v", err)
	}
	cl()
	if b, err := os.ReadFile(filepath.Join(dir, "gina", "hostfile.txt")); err != nil || string(b) != "en el host" {
		t.Errorf("el archivo no apareció en la carpeta del host: %v %q", err, b)
	}
	if err := a.DeleteSftpUser(svc.ID, "gina", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gina", "hostfile.txt")); err != nil {
		t.Errorf("deleteFiles=true no debe borrar carpetas del host: %v", err)
	}
	if err := a.DeleteSftpService(svc.ID, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gina", "hostfile.txt")); err != nil {
		t.Errorf("DeleteSftpService(removeData) no debe borrar la carpeta del host: %v", err)
	}
}

// Importar un contenedor atmoz plano (creado por la prueba): modo external, nunca se recrea ni se modifica.
func TestSftpImportPlainAtmozNeverRecreated(t *testing.T) {
	name := testutil.RandName("atmoz")
	cli := testutil.Docker(t)
	defer cli.Close()
	ctx := context.Background()
	p := nat.Port("22/tcp")
	resp, err := cli.Raw().ContainerCreate(ctx, &container.Config{Image: "atmoz/sftp:alpine", Cmd: []string{"ext:ExtPw-12345:1001"}, ExposedPorts: nat.PortSet{p: {}},
		Labels: map[string]string{testutil.TestLabel: "true"}},
		&container.HostConfig{PortBindings: nat.PortMap{p: []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ""}}}}, &network.NetworkingConfig{}, nil, name)
	if err != nil {
		t.Skipf("imagen atmoz no disponible: %v", err)
	}
	t.Cleanup(func() { testutil.RemoveByName(name) })
	if err := cli.Start(ctx, resp.ID); err != nil {
		t.Fatal(err)
	}
	a, _ := newSftpApp(t)
	imp, err := a.ListImportableSftpContainers()
	if err != nil {
		t.Fatal(err)
	}
	var found *ContainerInfo
	for i, c := range imp {
		t.Logf("importable SFTP detectado: %s (%s) puerto %d", c.Name, c.Image, c.HostPort)
		if c.Name == name {
			found = &imp[i]
		}
	}
	if found == nil || found.Engine != "sftp" {
		t.Fatalf("no se detectó el contenedor atmoz plano: %+v", imp)
	}
	svc, err := a.ImportSftpContainer(name)
	if err != nil {
		t.Fatal(err)
	}
	if svc.Mode != "external" || svc.Port != found.HostPort {
		t.Errorf("%+v", svc)
	}
	if _, err := a.ImportSftpContainer(name); err == nil {
		t.Error("doble importación debía fallar")
	}
	before, _ := cli.Raw().ContainerInspect(ctx, name)
	// usuarios no editables
	if us, err := a.ListSftpUsers(svc.ID); err != nil || len(us) != 0 {
		t.Errorf("ListSftpUsers external: %v %v", us, err)
	}
	if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: "nuevo", Password: "Pw-12345678"}); err == nil || !strings.Contains(err.Error(), "importado") {
		t.Errorf("CreateSftpUser external: %v", err)
	}
	if err := a.SetSftpUserPassword(svc.ID, "ext", "x"); err == nil {
		t.Error("SetSftpUserPassword external debía fallar")
	}
	if err := a.DeleteSftpUser(svc.ID, "ext", true); err == nil {
		t.Error("DeleteSftpUser external debía fallar")
	}
	// ciclo de vida permitido, login real, huellas por escaneo
	waitUntil(t, 30*time.Second, "ssh externo", func() bool {
		r, _ := a.TestSftpLogin(SftpLoginTest{ServiceID: svc.ID, User: "ext", Password: "ExtPw-12345"})
		return r.OK
	})
	if fp, err := a.GetSftpHostFingerprints(svc.ID); err != nil || len(fp) == 0 {
		t.Errorf("huellas externas: %v %v", fp, err)
	}
	if err := a.StopSftpService(svc.ID); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.SftpServiceStatus(svc.ID); s.State != "stopped" {
		t.Errorf("%+v", s)
	}
	if err := a.StartSftpService(svc.ID); err != nil {
		t.Fatal(err)
	}
	// DeleteSftpService con removeContainer/removeData sobre un servicio importado: solo quita el registro
	if err := a.DeleteSftpService(svc.ID, true, true); err != nil {
		t.Fatal(err)
	}
	after, err := cli.Raw().ContainerInspect(ctx, name)
	if err != nil {
		t.Fatalf("EL CONTENEDOR IMPORTADO FUE ELIMINADO por DeleteSftpService: %v", err)
	}
	if after.ID != before.ID || after.Created != before.Created {
		t.Errorf("el contenedor importado fue recreado: %s -> %s", before.ID, after.ID)
	}
	if l, _ := a.ListSftpServices(); len(l) != 0 {
		t.Errorf("debía quitarse del registro: %+v", l)
	}
	// y los contenedores sftp reales del usuario no fueron tocados (solo se verifican por inspección)
	for _, n := range []string{"sftp-iflow", "sftp_server"} {
		if info, err := cli.Raw().ContainerInspect(ctx, n); err == nil {
			t.Logf("%s sigue en estado %s (solo lectura)", n, info.State.Status)
		}
	}
}

func pasteKey() *SftpKeyRequest {
	g, _ := sftp.Generate("ed25519", "", "pegada")
	return &SftpKeyRequest{Mode: "paste", PublicKey: g.PublicLine}
}

// BUG (ALTA): un usuario SIN llaves (solo contraseña) recibe igualmente el montaje /home/<u>/.ssh/keys (carpeta vacía);
// el script create-sftp-user de atmoz hace `cat /home/<u>/.ssh/keys/*` y aborta con "No such file or directory",
// con lo que NO se crean los usuarios que vienen DESPUÉS en users.conf (no pueden iniciar sesión).
func TestSftpPasswordOnlyUserBreaksLaterUsers(t *testing.T) {
	a, _ := newSftpApp(t)
	svc := newSftpSvc(t, a, "sp", CreateSftpRequest{})
	for _, u := range []string{"first", "second", "third"} {
		if _, err := a.CreateSftpUser(svc.ID, CreateSftpUserRequest{Name: u, Password: "Pw-" + u + "-12345"}); err != nil {
			t.Fatal(err)
		}
	}
	waitUntil(t, 20*time.Second, "puerto", func() bool {
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(svc.Port), time.Second)
		if err == nil {
			c.Close()
		}
		return err == nil
	})
	for _, u := range []string{"first", "second", "third"} {
		r, _ := a.TestSftpLogin(SftpLoginTest{ServiceID: svc.ID, User: u, Password: "Pw-" + u + "-12345"})
		if !r.OK {
			lg, _ := a.SftpServiceLogs(svc.ID, 12)
			t.Errorf("el usuario solo-contraseña %q no puede iniciar sesión: %s / logs: %s", u, r.Message, lg)
		}
	}
}
