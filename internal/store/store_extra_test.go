package store

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// La contraseña en claro jamás debe aparecer en app.db (ni en el WAL).
func TestPasswordNeverPlaintextOnDisk(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	const pw = "Pl41n-S3cret!ZZZ"
	sv, err := s.CreateServer(Server{Name: "x", Engine: "postgres", Mode: "remote", Host: "h", Port: 1, AdminUser: "u"}, pw)
	if err != nil {
		t.Fatal(err)
	}
	s.UpdateServer(sv, pw+"2")
	s.LogOperation(Operation{ServerID: sv.ID, Kind: "k", Target: "t", Status: "ok"})
	s.Close() // checkpoint del WAL
	err = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		if bytes.Contains(b, []byte(pw)) {
			t.Errorf("contraseña en claro encontrada en %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecryptWithDifferentKeyFails(t *testing.T) {
	a := newTestStore(t)
	enc, _ := a.Encrypt("secreto")
	b, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := b.Decrypt(enc); err == nil {
		t.Fatal("otra clave no debe descifrar")
	}
	if _, err := a.Decrypt("###no-base64###"); err == nil {
		t.Fatal("base64 inválido debe fallar")
	}
	if _, err := a.Decrypt("AAAA"); err == nil {
		t.Fatal("dato corto debe fallar")
	}
	if v, err := a.Encrypt(""); v != "" || err != nil {
		t.Fatal("vacío -> vacío")
	}
	if v, err := a.Decrypt(""); v != "" || err != nil {
		t.Fatal("vacío -> vacío")
	}
}

func TestInvalidKeyFileRejected(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "secret.key"), []byte("corta"), 0o600)
	if _, err := OpenAt(dir); err == nil {
		t.Fatal("clave de longitud inválida debe fallar")
	} else if !strings.Contains(err.Error(), "clave") {
		t.Fatalf("mensaje en español esperado: %v", err)
	}
}

func TestKeyFilePermissions(t *testing.T) {
	s := newTestStore(t)
	st, err := os.Stat(filepath.Join(s.Dir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 32 {
		t.Fatalf("tamaño de clave %d", st.Size())
	}
	// en Windows los bits POSIX no aplican
	if mode := st.Mode().Perm(); mode&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("permisos demasiado abiertos: %v", mode)
	}
}

func TestUniqueNameIsCaseSensitive(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateServer(Server{Name: "Prod", Engine: "postgres", Mode: "remote"}, ""); err != nil {
		t.Fatal(err)
	}
	// Documenta el comportamiento: SQLite UNIQUE distingue mayúsculas, CreateLocal usa EqualFold.
	_, err := s.CreateServer(Server{Name: "prod", Engine: "postgres", Mode: "remote"}, "")
	t.Logf("segundo registro con distinta capitalización: err=%v", err)
}

func TestListServersOrderAndEmpty(t *testing.T) {
	s := newTestStore(t)
	l, err := s.ListServers()
	if err != nil || l == nil || len(l) != 0 {
		t.Fatalf("lista vacía debe ser slice no nil: %v %v", l, err)
	}
	s.CreateServer(Server{Name: "b", Engine: "mssql", Mode: "remote"}, "")
	s.CreateServer(Server{Name: "A", Engine: "mssql", Mode: "remote"}, "")
	l, _ = s.ListServers()
	if len(l) != 2 || l[0].Name != "A" {
		t.Fatalf("orden: %+v", l)
	}
}
