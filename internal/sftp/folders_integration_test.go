//go:build integration

package sftp

import (
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"dbmanager/internal/testutil"

	psftp "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func sftpClient(t *testing.T, port int, user, pw string) (*psftp.Client, func()) {
	t.Helper()
	c, err := ssh.Dial("tcp", net.JoinHostPort("localhost", strconv.Itoa(port)), &ssh.ClientConfig{User: user,
		Auth: []ssh.AuthMethod{ssh.Password(pw)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("ssh %s: %v", user, err)
	}
	cl, err := psftp.NewClient(c)
	if err != nil {
		c.Close()
		t.Fatal(err)
	}
	return cl, func() { cl.Close(); c.Close() }
}

func hasEntry(cl *psftp.Client, dir, name string) bool {
	es, err := cl.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range es {
		if e.Name() == name {
			return true
		}
	}
	return false
}

func writeFile(cl *psftp.Client, p, content string) error {
	f, err := cl.Create(p)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte(content)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func runFolderFlow(t *testing.T, mode string) {
	m := newMgr(t)
	ctx := context.Background()
	name := strings.TrimPrefix(testutil.RandName("sftpf"), "dbmanager-")
	cname := "dbmanager-" + slug(name)
	data := ""
	if mode == "bind" {
		d, err := os.MkdirTemp(os.TempDir(), "dbm-sftpf-")
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
			testutil.RemoveVolume("dbmanager-" + slug(name) + "-data-shared-compartida")
		}
	})
	svc, err := m.Create(ctx, CreateRequest{Name: name, DataMode: mode, DataPath: data})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	alicePw, bobPw := "Alice!Passw0rd", "Bob!Passw0rd1"
	if _, err := m.CreateUser(ctx, svc.ID, UserRequest{Name: "alice", Password: alicePw}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateUser(ctx, svc.ID, UserRequest{Name: "bob", Password: bobPw}); err != nil {
		t.Fatal(err)
	}

	if err := m.CreateFolder(ctx, svc.ID, "compartida"); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	for _, bad := range []string{"compartida", "upload", ".ssh", "a/b", ""} {
		if err := m.CreateFolder(ctx, svc.ID, bad); err == nil {
			t.Errorf("CreateFolder(%q) debería fallar", bad)
		}
	}
	// Sin acceso nadie la ve.
	cl, done := sftpClient(t, svc.Port, "bob", bobPw)
	if hasEntry(cl, ".", "compartida") {
		t.Error("sin acceso, bob no debería ver la carpeta")
	}
	done()

	if err := m.SetFolderAccess(ctx, svc.ID, "compartida", "alice", "readwrite"); err != nil {
		t.Fatalf("alice readwrite: %v", err)
	}
	if err := m.SetFolderAccess(ctx, svc.ID, "compartida", "bob", "read"); err != nil {
		t.Fatalf("bob read: %v", err)
	}
	if err := m.SetFolderAccess(ctx, svc.ID, "compartida", "bob", "bogus"); err == nil {
		t.Error("nivel inválido debería fallar")
	}

	ac, done := sftpClient(t, svc.Port, "alice", alicePw)
	if err := writeFile(ac, "compartida/doc.txt", "contenido compartido"); err != nil {
		t.Fatalf("alice debería poder escribir: %v", err)
	}
	if err := writeFile(ac, "upload/mio.txt", "privado"); err != nil {
		t.Fatalf("alice upload: %v", err)
	}
	done()

	bc, done := sftpClient(t, svc.Port, "bob", bobPw)
	if !hasEntry(bc, ".", "compartida") {
		t.Fatal("bob debería ver la carpeta")
	}
	if !hasEntry(bc, "compartida", "doc.txt") {
		t.Error("bob debería listar doc.txt")
	}
	r, err := bc.Open("compartida/doc.txt")
	if err != nil {
		t.Fatalf("bob debería poder leer: %v", err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "contenido compartido" {
		t.Errorf("contenido = %q", b)
	}
	if err := writeFile(bc, "compartida/bob.txt", "x"); err == nil {
		t.Error("bob (read) NO debería poder subir")
	}
	if err := bc.Remove("compartida/doc.txt"); err == nil {
		t.Error("bob (read) NO debería poder borrar")
	}
	done()

	fs, err := m.ListFolders(svc.ID)
	if err != nil || len(fs) != 1 || len(fs[0].Access) != 2 {
		t.Fatalf("ListFolders: %+v %v", fs, err)
	}

	// bob none: no la ve; alice conserva el archivo.
	if err := m.SetFolderAccess(ctx, svc.ID, "compartida", "bob", "none"); err != nil {
		t.Fatal(err)
	}
	bc, done = sftpClient(t, svc.Port, "bob", bobPw)
	if hasEntry(bc, ".", "compartida") {
		t.Error("bob (none) no debería ver la carpeta")
	}
	done()
	ac, done = sftpClient(t, svc.Port, "alice", alicePw)
	if !hasEntry(ac, "compartida", "doc.txt") {
		t.Error("el archivo debería persistir tras recrear")
	}
	if !hasEntry(ac, "upload", "mio.txt") {
		t.Error("el upload propio debería persistir")
	}
	done()

	// Colisiones usuario/carpeta
	if _, err := m.CreateUser(ctx, svc.ID, UserRequest{Name: "shared-compartida", Password: "Zed!Passw0rd12"}); err == nil {
		t.Error("usuario shared-compartida debería rechazarse")
	}

	// Eliminar carpeta
	if err := m.DeleteFolder(ctx, svc.ID, "compartida", true); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	ac, done = sftpClient(t, svc.Port, "alice", alicePw)
	if hasEntry(ac, ".", "compartida") {
		t.Error("tras eliminar, alice no debería ver la carpeta")
	}
	done()
	if fs, _ := m.ListFolders(svc.ID); len(fs) != 0 {
		t.Errorf("quedan carpetas: %+v", fs)
	}
	if err := m.Delete(ctx, svc.ID, true, true); err != nil {
		t.Fatal(err)
	}
}

func TestSftpFoldersVolumeMode(t *testing.T) { runFolderFlow(t, "volume") }
func TestSftpFoldersBindMode(t *testing.T)   { runFolderFlow(t, "bind") }
