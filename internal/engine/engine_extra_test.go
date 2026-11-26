package engine_test

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"dbmanager/internal/engine"
	"dbmanager/internal/engine/mssql"
	"dbmanager/internal/engine/postgres"

	"github.com/jackc/pgx/v5"
)

var nastyPasswords = []string{
	"p@ss/word", "a:b@c/d?e#f%g&h=i", "x'; DROP LOGIN sa; --", `back\slash"quote'`, "ñandú€日本", "sp ace", "100%25", "+plus+", "[]{}",
}

func TestPostgresConnURLRoundTrip(t *testing.T) {
	for _, pw := range nastyPasswords {
		for _, host := range []string{"localhost", "::1", "db.example.com"} {
			c := engine.Conn{Host: host, Port: 5432, User: "us@er", Password: pw}
			u := postgres.ConnURL(c, "mi_db")
			cfg, err := pgx.ParseConfig(u)
			if err != nil {
				t.Fatalf("ParseConfig(%q): %v", pw, err)
			}
			if cfg.Password != pw || cfg.User != "us@er" || cfg.Database != "mi_db" || int(cfg.Port) != 5432 {
				t.Errorf("round-trip falló para %q host %s: %+v", pw, host, cfg)
			}
		}
	}
}

func TestMSSQLDSNRoundTrip(t *testing.T) {
	for _, pw := range nastyPasswords {
		d := mssql.DSN(engine.Conn{Host: "::1", Port: 1433, User: "s@a", Password: pw}, "master")
		u, err := url.Parse(d)
		if err != nil {
			t.Fatalf("%q: %v", pw, err)
		}
		got, _ := u.User.Password()
		if got != pw || u.User.Username() != "s@a" {
			t.Errorf("password %q -> %q", pw, got)
		}
		if u.Host != "[::1]:1433" {
			t.Errorf("host %q", u.Host)
		}
	}
}

func TestTLSModes(t *testing.T) {
	cases := []struct {
		tls, trust bool
		pg         string
	}{
		{false, false, "sslmode=prefer"}, {true, false, "sslmode=verify-full"}, {true, true, "sslmode=require"},
	}
	for _, c := range cases {
		u := postgres.ConnURL(engine.Conn{Host: "h", Port: 1, User: "u", Password: "p", TLS: c.tls, TrustCert: c.trust}, "d")
		if !strings.Contains(u, c.pg) {
			t.Errorf("tls=%v trust=%v: %s", c.tls, c.trust, u)
		}
	}
	d := mssql.DSN(engine.Conn{Host: "h", Port: 1, User: "u", Password: "p", TLS: true}, "")
	if !strings.Contains(d, "encrypt=true") || strings.Contains(d, "TrustServerCertificate") {
		t.Error(d)
	}
	_ = strconv.Itoa
}

func TestValidLevel(t *testing.T) {
	for _, l := range []string{"owner", "readwrite", "readonly", "none"} {
		if !engine.ValidLevel(l) {
			t.Error(l)
		}
	}
	for _, l := range []string{"", "OWNER", "admin", "none; DROP"} {
		if engine.ValidLevel(l) {
			t.Error(l)
		}
	}
}

func TestMSSQLSystemLoginEdgeCases(t *testing.T) {
	for _, n := range []string{"sa", "SA", "Sa", "##MS_x##", "nt service\\x", "NT AUTHORITY\\SYSTEM"} {
		if !mssql.IsSystemLogin(n) {
			t.Errorf("%q debería ser de sistema", n)
		}
	}
	for _, n := range []string{"sa2", "saa", "ntuser", "user"} {
		if mssql.IsSystemLogin(n) {
			t.Errorf("%q no debería ser de sistema", n)
		}
	}
}

func TestPostgresSystemRoleEdgeCases(t *testing.T) {
	for _, n := range []string{"postgres", "POSTGRES", "pg_monitor", "pg_signal_backend"} {
		if !postgres.IsSystemRole(n) {
			t.Errorf("%q debería ser de sistema", n)
		}
	}
	if postgres.IsSystemRole("pgadmin") || postgres.IsSystemRole("pg") {
		t.Error("falso positivo")
	}
}

func TestMSSQLRestoreSQLInjectionSafe(t *testing.T) {
	mv := []mssql.Move{{Logical: "x'); DROP DATABASE master; --", Physical: "/d/it's.mdf"}}
	q := mssql.BuildRestoreSQL("/b/a'b.bak", "t]x", mv, false)
	for _, bad := range []string{"N'x'); DROP", "[t]x]"} {
		if strings.Contains(q, bad) {
			t.Errorf("SQL sin escapar: %s", q)
		}
	}
	if !strings.Contains(q, "[t]]x]") || !strings.Contains(q, "N'x''); DROP DATABASE master; --'") {
		t.Errorf("escape inesperado: %s", q)
	}
}
