package mssql

import (
	"strings"
	"testing"

	"dbmanager/internal/engine"
)

func TestClassifyLevel(t *testing.T) {
	cases := []struct {
		o, r, w, u bool
		want       string
	}{
		{true, false, false, false, "owner"}, {false, true, true, true, "readwrite"}, {false, true, false, true, "readonly"},
		{false, false, true, true, "custom"}, {false, false, false, true, "custom"}, {false, false, false, false, "none"},
		{false, false, true, false, "none"}, {true, false, false, true, "owner"},
	}
	for _, c := range cases {
		if got := ClassifyLevel(c.o, c.r, c.w, c.u); got != c.want {
			t.Errorf("%v -> %s, want %s", c, got, c.want)
		}
	}
}

func TestRolesForLevel(t *testing.T) {
	if r := RolesForLevel("readwrite"); len(r) != 2 || r[0] != "db_datareader" || r[1] != "db_datawriter" {
		t.Fatal(r)
	}
	if RolesForLevel("none") != nil {
		t.Fatal("none no tiene roles")
	}
}

func TestSystemNames(t *testing.T) {
	for _, n := range []string{"master", "TempDB", "model", "msdb"} {
		if !IsSystemDB(n) {
			t.Errorf("%s", n)
		}
	}
	if IsSystemDB("APIManager") {
		t.Error("usuario")
	}
	for _, n := range []string{"sa", "##MS_PolicyEventProcessingLogin##", "NT SERVICE\\MSSQLSERVER", "NT AUTHORITY\\SYSTEM"} {
		if !IsSystemLogin(n) {
			t.Errorf("%s", n)
		}
	}
	if IsSystemLogin("app_user") {
		t.Error("usuario")
	}
}

func TestParsePercent(t *testing.T) {
	if p, ok := ParsePercent("45 percent processed."); !ok || p != 45 {
		t.Fatal(p, ok)
	}
	if _, ok := ParsePercent("RESTORE DATABASE successfully processed 100 pages"); ok {
		t.Fatal("no debe coincidir")
	}
}

func TestPlanMovesAndSQL(t *testing.T) {
	files := []engine.BackupFile{{LogicalName: "A", Type: "data"}, {LogicalName: "A_log", Type: "log"}, {LogicalName: "B", Type: "data"}}
	mv := PlanMoves(files, "nuevo", "/var/opt/mssql/data/", "/var/opt/mssql/data")
	if mv[0].Physical != "/var/opt/mssql/data/nuevo.mdf" || mv[1].Physical != "/var/opt/mssql/data/nuevo_log.ldf" || mv[2].Physical != "/var/opt/mssql/data/nuevo_2.ndf" {
		t.Fatalf("%+v", mv)
	}
	if got := PlanMoves(files[:1], "x", `C:\Data`, `C:\Data`)[0].Physical; got != `C:\Data\x.mdf` {
		t.Fatal(got)
	}
	q := BuildRestoreSQL("/b/it's.bak", "nuevo", mv[:1], true)
	want := "RESTORE DATABASE [nuevo] FROM DISK = N'/b/it''s.bak' WITH MOVE N'A' TO N'/var/opt/mssql/data/nuevo.mdf', REPLACE, STATS = 5"
	if q != want {
		t.Fatalf("got %s", q)
	}
	if strings.Contains(BuildRestoreSQL("/b.bak", "n", mv[:1], false), "REPLACE") {
		t.Fatal("sin replace")
	}
}

func TestDSN(t *testing.T) {
	d := DSN(engine.Conn{Host: "localhost", Port: 14331, User: "sa", Password: "p@ss/word", TLS: true, TrustCert: true}, "master")
	if !strings.Contains(d, "localhost:14331") || !strings.Contains(d, "TrustServerCertificate=true") || !strings.Contains(d, "encrypt=true") {
		t.Fatal(d)
	}
	if strings.Contains(d, "p@ss/word") {
		t.Fatal("la contraseña debe ir codificada")
	}
}
