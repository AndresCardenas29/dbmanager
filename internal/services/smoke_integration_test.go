//go:build integration

package services

import (
	"context"
	"os"
	"testing"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/store"
)

// Humo manual: crea contenedores dbmanager-dev-* propios, ejercita el flujo completo y los elimina.
// Ejecutar: go test -tags integration -run TestSmoke -v -timeout 30m ./internal/services

func newSvc(t *testing.T) *Services {
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
	return &Services{Store: st, Docker: func() (*dockerx.Client, error) { return cli, nil },
		Emit: func(ev string, p Progress) { t.Logf("%s %3d%% %s", ev, p.Percent, p.Message) }}
}

func cleanup(t *testing.T, s *Services, cname, volume string) {
	cli, _ := s.Docker()
	ctx := context.Background()
	cli.Remove(ctx, cname, true)
	if volume != "" {
		time.Sleep(time.Second)
		cli.RemoveVolume(ctx, volume)
	}
}

func runFlow(t *testing.T, s *Services, req CreateRequest, cname, vol string) {
	t.Cleanup(func() { cleanup(t, s, cname, vol) })
	ctx := context.Background()
	sv, err := s.CreateLocal(ctx, req)
	if err != nil {
		t.Fatalf("CreateLocal: %v", err)
	}
	t.Logf("servicio %s en puerto %d", sv.Name, sv.Port)
	st := s.TestConnection(ctx, sv)
	if !st.Reachable {
		t.Fatalf("TestConnection: %+v", st)
	}
	t.Log(st.Version)

	e, err := s.Engine(sv)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	must("CreateDatabase", e.CreateDatabase(ctx, "smoke_db", ""))
	must("CreateLogin", e.CreateLogin(ctx, "smoke_user", "Sm0ke!Passw0rd", nil))
	must("SetServerRoles", e.SetServerRoles(ctx, "smoke_user", []string{"CREATEDB"}[:0]))
	for _, lv := range []string{"readwrite", "readonly", "owner", "readonly", "none"} {
		must("SetDatabaseAccess "+lv, e.SetDatabaseAccess(ctx, "smoke_user", "smoke_db", lv))
		m, err := e.GetPermissionMatrix(ctx)
		must("matrix", err)
		got := ""
		for _, c := range m.Cells {
			if c.Login == "smoke_user" && c.Database == "smoke_db" {
				got = c.Level
			}
		}
		if got != lv {
			t.Errorf("nivel esperado %s, matriz %q", lv, got)
		}
	}
	dbs, err := e.ListDatabases(ctx)
	must("ListDatabases", err)
	t.Logf("bases: %+v", dbs)
	lg, err := e.ListLogins(ctx)
	must("ListLogins", err)
	t.Logf("logins: %+v", lg)

	dest := t.TempDir()
	b, err := s.Backup(ctx, sv, "smoke_db", dest, "req1")
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	t.Logf("backup: %s (%d bytes)", b.FilePath, b.SizeBytes)
	if _, err := os.Stat(b.FilePath); err != nil {
		t.Fatal(err)
	}
	info, err := s.Inspect(ctx, sv, b.FilePath)
	must("Inspect", err)
	t.Logf("inspect: %+v", info)

	r, err := s.Restore(ctx, sv, RestoreRequest{RequestID: "req2", FilePath: b.FilePath, TargetDatabase: "smoke_db_restored"})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	t.Logf("restore: %+v", r)
	// ya existe y sin replace -> error
	if _, err := s.Restore(ctx, sv, RestoreRequest{FilePath: b.FilePath, TargetDatabase: "smoke_db_restored"}); err == nil {
		t.Error("restaurar sobre existente sin replace debe fallar")
	}
	if _, err := s.Restore(ctx, sv, RestoreRequest{FilePath: b.FilePath, TargetDatabase: "smoke_db_restored", Replace: true}); err != nil {
		t.Errorf("restore con replace: %v", err)
	}
	ok, err := e.DatabaseExists(ctx, "smoke_db_restored")
	if err != nil || !ok {
		t.Fatalf("la base restaurada no existe: %v", err)
	}
	must("DropDatabase", e.DropDatabase(ctx, "smoke_db_restored"))
	must("DropDatabase", e.DropDatabase(ctx, "smoke_db"))
	must("ChangePassword", e.ChangePassword(ctx, "smoke_user", "N3w!Passw0rd"))
	must("DropLogin", e.DropLogin(ctx, "smoke_user"))
	if _, err := s.Store.ListRestores(0); err != nil {
		t.Fatal(err)
	}
}

func TestSmokePostgres(t *testing.T) {
	s := newSvc(t)
	runFlow(t, s, CreateRequest{Name: "dev-pg", Engine: "postgres", AdminPassword: "Pg!Passw0rd"}, "dbmanager-dev-pg", "dbmanager-dev-pg-data")
}

func TestSmokeMSSQL(t *testing.T) {
	s := newSvc(t)
	runFlow(t, s, CreateRequest{Name: "dev-mssql", Engine: "mssql", AdminPassword: "Str0ng!Passw0rd"}, "dbmanager-dev-mssql", "dbmanager-dev-mssql-data")
}
