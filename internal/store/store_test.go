package store

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DBMANAGER_DATA_DIR", dir)
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenCreatesFilesAndMigrates(t *testing.T) {
	s := newTestStore(t)
	if s.Dir() != os.Getenv("DBMANAGER_DATA_DIR") {
		t.Fatalf("dir inesperado: %s", s.Dir())
	}
	for _, f := range []string{"app.db", "secret.key"} {
		if _, err := os.Stat(filepath.Join(s.Dir(), f)); err != nil {
			t.Fatalf("falta %s: %v", f, err)
		}
	}
	v, err := s.SchemaVersion()
	if err != nil || v != len(migrations) {
		t.Fatalf("versión=%d err=%v", v, err)
	}
	// reabrir no re-aplica migraciones y conserva la clave
	key := append([]byte(nil), s.key...)
	s.Close()
	s2, err := OpenAt(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if string(s2.key) != string(key) {
		t.Fatal("la clave cambió al reabrir")
	}
}

func TestEncryptRoundTrip(t *testing.T) {
	s := newTestStore(t)
	enc, err := s.Encrypt("S3cret!Pass")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "" || enc == "S3cret!Pass" {
		t.Fatal("no se cifró")
	}
	enc2, _ := s.Encrypt("S3cret!Pass")
	if enc == enc2 {
		t.Fatal("el nonce debe ser aleatorio")
	}
	got, err := s.Decrypt(enc)
	if err != nil || got != "S3cret!Pass" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err := s.Decrypt(enc[:len(enc)-4] + "AAAA"); err == nil {
		t.Fatal("debió fallar con datos alterados")
	}
}

func TestSettings(t *testing.T) {
	s := newTestStore(t)
	if v, _ := s.GetSetting("x"); v != "" {
		t.Fatal("esperaba vacío")
	}
	s.SetSetting("lastDir", "C:\\a")
	s.SetSetting("lastDir", "C:\\b")
	m, err := s.GetSettings()
	if err != nil || m["lastDir"] != "C:\\b" || len(m) != 1 {
		t.Fatalf("%v %v", m, err)
	}
}

func TestServerCRUD(t *testing.T) {
	s := newTestStore(t)
	sv, err := s.CreateServer(Server{Name: "pg1", Engine: "postgres", Mode: "local", Host: "localhost", Port: 5432,
		AdminUser: "postgres", ContainerName: "pg1", Image: "postgres:16", DataMode: "volume", DataPath: "pg1-data",
		TLS: true, PgDefaultDB: "postgres"}, "Secreta123!")
	if err != nil {
		t.Fatal(err)
	}
	if sv.ID == 0 || sv.AdminPasswordEnc == "" || sv.AdminPasswordEnc == "Secreta123!" || sv.CreatedAt.IsZero() {
		t.Fatalf("server inválido: %+v", sv)
	}
	if pw, _ := s.ServerPassword(sv); pw != "Secreta123!" {
		t.Fatalf("password=%q", pw)
	}
	if !sv.TLS || sv.TrustServerCert {
		t.Fatal("flags incorrectos")
	}
	if _, err := s.CreateServer(Server{Name: "pg1", Engine: "postgres", Mode: "remote"}, ""); err == nil {
		t.Fatal("nombre duplicado debería fallar")
	}

	sv.Name = "pg-renombrado"
	sv.TrustServerCert = true
	up, err := s.UpdateServer(sv, "")
	if err != nil {
		t.Fatal(err)
	}
	if up.Name != "pg-renombrado" || !up.TrustServerCert {
		t.Fatalf("update: %+v", up)
	}
	if pw, _ := s.ServerPassword(up); pw != "Secreta123!" {
		t.Fatal("la contraseña debió conservarse")
	}
	up, _ = s.UpdateServer(up, "Nueva456!")
	if pw, _ := s.ServerPassword(up); pw != "Nueva456!" {
		t.Fatal("la contraseña debió cambiar")
	}

	list, err := s.ListServers()
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%d err=%v", len(list), err)
	}
	if err := s.DeleteServer(sv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetServer(sv.ID); err != ErrNotFound {
		t.Fatalf("esperaba ErrNotFound, got %v", err)
	}
	if err := s.DeleteServer(sv.ID); err != ErrNotFound {
		t.Fatal("borrar inexistente debe dar ErrNotFound")
	}
}

func TestBackupsRestoresOperations(t *testing.T) {
	s := newTestStore(t)
	b1, err := s.CreateBackup(Backup{ServerID: 1, ServerName: "a", Database: "db1", FilePath: "/x/1.bak", SizeBytes: 10, Status: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	s.CreateBackup(Backup{ServerID: 2, ServerName: "b", Database: "db2", Status: "running"})
	if l, _ := s.ListBackups(0); len(l) != 2 || l[0].Database != "db2" {
		t.Fatalf("todos: %+v", l)
	}
	if l, _ := s.ListBackups(1); len(l) != 1 || l[0].ID != b1.ID {
		t.Fatalf("filtrado: %+v", l)
	}
	b1.Status, b1.Message = "error", "falló"
	s.UpdateBackup(b1)
	if g, _ := s.GetBackup(b1.ID); g.Status != "error" || g.Message != "falló" {
		t.Fatalf("%+v", g)
	}

	r, err := s.CreateRestore(Restore{ServerID: 1, ServerName: "a", SourceFile: "/x/1.bak", TargetDatabase: "db1_r", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.FinishedAt.IsZero() {
		t.Fatal("finishedAt debe ser cero")
	}
	s.FinishRestore(r.ID, "ok", "listo")
	g, _ := s.GetRestore(r.ID)
	if g.Status != "ok" || g.FinishedAt.IsZero() {
		t.Fatalf("%+v", g)
	}
	if l, _ := s.ListRestores(0); len(l) != 1 {
		t.Fatal("restores")
	}

	for i := 0; i < 5; i++ {
		s.LogOperation(Operation{ServerID: 1, Kind: "start", Target: "a", Status: "ok"})
	}
	ops, err := s.ListOperations(3)
	if err != nil || len(ops) != 3 || ops[0].ID < ops[1].ID {
		t.Fatalf("ops=%+v err=%v", ops, err)
	}
}

func TestSftpFolders(t *testing.T) {
	s := newTestStore(t)
	svc, err := s.CreateSftpService(SftpService{Name: "s", Mode: "managed", ContainerName: "c", Image: "i", DataMode: "volume", DataPath: "p"})
	if err != nil {
		t.Fatal(err)
	}
	u1, err := s.CreateSftpUser(SftpUser{ServiceID: svc.ID, Name: "alice", UploadDir: "upload"})
	if err != nil {
		t.Fatal(err)
	}
	u2, _ := s.CreateSftpUser(SftpUser{ServiceID: svc.ID, Name: "bob", UploadDir: "upload"})
	f, err := s.CreateSftpFolder(svc.ID, "docs")
	if err != nil || f.ID == 0 {
		t.Fatal(err)
	}
	if _, err := s.CreateSftpFolder(svc.ID, "docs"); err == nil {
		t.Fatal("duplicado debe fallar")
	}
	if g, err := s.GetSftpFolder(svc.ID, "docs"); err != nil || g.ID != f.ID {
		t.Fatal(err)
	}
	if _, err := s.GetSftpFolder(svc.ID, "zzz"); err != ErrNotFound {
		t.Fatalf("esperaba ErrNotFound: %v", err)
	}
	if err := s.SetSftpFolderAccess(f.ID, u1.ID, "readwrite"); err != nil {
		t.Fatal(err)
	}
	s.SetSftpFolderAccess(f.ID, u2.ID, "read")
	s.SetSftpFolderAccess(f.ID, u1.ID, "read") // upsert
	acc, err := s.ListSftpFolderAccess(svc.ID)
	if err != nil || len(acc) != 2 || acc[0].UserName != "alice" || acc[0].Level != "read" || acc[1].UserName != "bob" {
		t.Fatalf("%+v %v", acc, err)
	}
	if s.SetSftpFolderAccess(f.ID, u1.ID, "bogus") == nil {
		t.Fatal("nivel inválido")
	}
	s.SetSftpFolderAccess(f.ID, u1.ID, "none")
	if acc, _ = s.ListSftpFolderAccess(svc.ID); len(acc) != 1 {
		t.Fatalf("%+v", acc)
	}
	// borrar usuario: cascade
	if err := s.DeleteSftpUser(u2.ID); err != nil {
		t.Fatal(err)
	}
	if acc, _ = s.ListSftpFolderAccess(svc.ID); len(acc) != 0 {
		t.Fatalf("cascade usuario: %+v", acc)
	}
	s.SetSftpFolderAccess(f.ID, u1.ID, "read")
	if err := s.DeleteSftpFolder(f.ID); err != nil {
		t.Fatal(err)
	}
	if fs, _ := s.ListSftpFolders(svc.ID); len(fs) != 0 {
		t.Fatal("no se borró")
	}
	if acc, _ = s.ListSftpFolderAccess(svc.ID); len(acc) != 0 {
		t.Fatal("cascade carpeta")
	}
	if s.DeleteSftpFolder(f.ID) != ErrNotFound {
		t.Fatal("ErrNotFound")
	}
}
