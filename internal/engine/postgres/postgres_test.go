package postgres

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dbmanager/internal/engine"
)

func TestDetectFormat(t *testing.T) {
	if DetectFormat([]byte("PGDMP\x01\x0e")) != FormatCustom {
		t.Fatal("custom")
	}
	if DetectFormat([]byte("-- PostgreSQL database dump")) != FormatPlain || DetectFormat(nil) != FormatPlain {
		t.Fatal("plain")
	}
	p := filepath.Join(t.TempDir(), "a.dump")
	os.WriteFile(p, []byte("PGDMPxxxx"), 0o600)
	if f, err := DetectFormatFile(p); err != nil || f != FormatCustom {
		t.Fatal(f, err)
	}
}

func TestClassifyLevel(t *testing.T) {
	if ClassifyLevel(true, false, false, false) != "owner" ||
		ClassifyLevel(false, true, true, true) != "readwrite" ||
		ClassifyLevel(false, true, true, false) != "readonly" ||
		ClassifyLevel(false, false, false, false) != "none" {
		t.Fatal("clasificación")
	}
}

func TestSystemNames(t *testing.T) {
	for _, n := range []string{"template0", "Template1", "postgres"} {
		if !IsSystemDB(n) {
			t.Error(n)
		}
	}
	if IsSystemDB("mydb") || !IsSystemRole("pg_read_all_data") || !IsSystemRole("postgres") || IsSystemRole("app") {
		t.Fatal("nombres")
	}
}

func TestCommands(t *testing.T) {
	got := DumpCmd("", 0, "postgres", "db1", "/tmp/x.dump")
	want := []string{"pg_dump", "-Fc", "--no-password", "-U", "postgres", "-f", "/tmp/x.dump", "db1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	r := RestoreCmd(FormatCustom, "h", 5432, "u", "t", "/f")
	if r[0] != "pg_restore" || !strings.Contains(strings.Join(r, " "), "-h h -p 5432") {
		t.Fatal(r)
	}
	if RestoreCmd(FormatPlain, "", 0, "u", "t", "/f")[0] != "psql" {
		t.Fatal("psql")
	}
	if MajorFromVersion("PostgreSQL 16.4 (Debian)") != 16 || MajorFromVersion("17.0") != 17 || MajorFromVersion("x") != 0 {
		t.Fatal("major")
	}
}

func TestSQLBuilders(t *testing.T) {
	rv := strings.Join(revokeSQL(`we"ird`), ";")
	if !strings.Contains(rv, `"we""ird"`) {
		t.Fatal(rv)
	}
	if len(grantSQL("a", engine.LevelReadWrite)) == 0 || grantSQL("a", engine.LevelNone) != nil {
		t.Fatal("grants")
	}
	u := ConnURL(engine.Conn{Host: "h", Port: 5432, User: "u", Password: "p/w@", TLS: true, TrustCert: true}, "db")
	if !strings.Contains(u, "sslmode=require") || strings.Contains(u, "p/w@") {
		t.Fatal(u)
	}
}
