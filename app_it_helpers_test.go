//go:build integration

package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/engine/mssql"
	"dbmanager/internal/engine/postgres"
	"dbmanager/internal/testutil"

	"github.com/docker/docker/api/types/container"
	"github.com/jackc/pgx/v5"
	_ "github.com/microsoft/go-mssqldb"
)

// Pruebas de integración de la app: se ejecutan con
//   go test -tags integration -v -timeout 30m .
// Usan contenedores propios dbmanager-test-* (un mssql y un postgres compartidos por todo el paquete,
// más los que cada prueba cree) y un DBMANAGER_DATA_DIR temporal por prueba.

var (
	engines = []string{"postgres", "mssql"}

	sharedMu sync.Mutex
	shared   = map[string]*sharedC{}
)

type sharedC struct {
	once sync.Once
	c    testutil.Container
	err  error
}

func TestMain(m *testing.M) {
	testutil.SweepOlderThan(15 * time.Minute)
	code := m.Run()
	sharedMu.Lock()
	for _, s := range shared {
		if s.c.Name != "" {
			testutil.RemoveByName(s.c.Name)
		}
	}
	sharedMu.Unlock()
	os.Exit(code)
}

// sharedContainer devuelve (arrancándolo una sola vez) el contenedor compartido del motor.
func sharedContainer(t testing.TB, eng string) testutil.Container {
	t.Helper()
	sharedMu.Lock()
	s := shared[eng]
	if s == nil {
		s = &sharedC{}
		shared[eng] = s
	}
	sharedMu.Unlock()
	s.once.Do(func() {
		name := testutil.RandName("shared-" + eng)
		pw := testutil.PGPassword
		if eng == "mssql" {
			pw = testutil.MSSQLPassword
		}
		s.c, s.err = testutil.StartPlain(testutil.PlainOptions{Name: name, Engine: eng, Password: pw})
	})
	if s.err != nil {
		t.Fatalf("no se pudo iniciar el contenedor compartido de %s: %v", eng, s.err)
	}
	return s.c
}

func uniq(prefix string) string {
	b := make([]byte, 4)
	rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

// importShared registra el contenedor compartido en la app (usa ImportContainer: ejercita ese flujo).
func importShared(t testing.TB, a *App, c testutil.Container) Server {
	t.Helper()
	sv, err := a.ImportContainer(ImportContainerRequest{ContainerName: c.Name, AdminUser: c.User, AdminPassword: c.Password})
	if err != nil {
		t.Fatalf("ImportContainer(%s): %v", c.Name, err)
	}
	return sv
}

// rawDB abre una conexión SQL directa (fuera de la app) para preparar y verificar datos.
type rawDB struct {
	c      testutil.Container
	user   string
	pw     string
	dbname string
}

func (r rawDB) exec(stmt string, args ...any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	switch r.c.Engine {
	case "postgres":
		cn := r.c.Conn()
		cn.User, cn.Password = r.user, r.pw
		db := r.dbname
		if db == "" {
			db = "postgres"
		}
		conn, err := pgx.Connect(ctx, postgres.ConnURL(cn, db))
		if err != nil {
			return err
		}
		defer conn.Close(ctx)
		_, err = conn.Exec(ctx, stmt, args...)
		return err
	default:
		cn := r.c.Conn()
		cn.User, cn.Password = r.user, r.pw
		db, err := sql.Open("sqlserver", mssql.DSN(cn, r.dbname))
		if err != nil {
			return err
		}
		defer db.Close()
		_, err = db.ExecContext(ctx, stmt, args...)
		return err
	}
}

func (r rawDB) queryInt(q string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var n int64
	switch r.c.Engine {
	case "postgres":
		cn := r.c.Conn()
		cn.User, cn.Password = r.user, r.pw
		db := r.dbname
		if db == "" {
			db = "postgres"
		}
		conn, err := pgx.Connect(ctx, postgres.ConnURL(cn, db))
		if err != nil {
			return 0, err
		}
		defer conn.Close(ctx)
		err = conn.QueryRow(ctx, q).Scan(&n)
		return n, err
	default:
		cn := r.c.Conn()
		cn.User, cn.Password = r.user, r.pw
		db, err := sql.Open("sqlserver", mssql.DSN(cn, r.dbname))
		if err != nil {
			return 0, err
		}
		defer db.Close()
		err = db.QueryRowContext(ctx, q).Scan(&n)
		return n, err
	}
}

func admin(c testutil.Container, db string) rawDB {
	return rawDB{c: c, user: c.User, pw: c.Password, dbname: db}
}

func asUser(c testutil.Container, user, pw, db string) rawDB {
	return rawDB{c: c, user: user, pw: pw, dbname: db}
}

// qid cita un identificador según el motor.
func qid(eng, name string) string {
	if eng == "mssql" {
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func qstr(eng, s string) string {
	if eng == "mssql" {
		return "N'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// mkDB crea una base con una tabla t(id,v) y n filas (usa el admin crudo; "setup").
func mkDB(t testing.TB, c testutil.Container, name string, rows int) {
	t.Helper()
	if err := admin(c, "").exec("CREATE DATABASE " + qid(c.Engine, name)); err != nil {
		t.Fatalf("crear base %s: %v", name, err)
	}
	t.Cleanup(func() { dropDBRaw(c, name) })
	if err := admin(c, name).exec("CREATE TABLE t (id int, v varchar(50))"); err != nil {
		t.Fatalf("crear tabla en %s: %v", name, err)
	}
	for i := 1; i <= rows; i++ {
		if err := admin(c, name).exec(fmt.Sprintf("INSERT INTO t VALUES (%d, 'fila%d')", i, i)); err != nil {
			t.Fatal(err)
		}
	}
}

func dropDBRaw(c testutil.Container, name string) {
	switch c.Engine {
	case "postgres":
		admin(c, "").exec("DROP DATABASE IF EXISTS " + qid("postgres", name) + " WITH (FORCE)")
	default:
		admin(c, "").exec("IF DB_ID(" + qstr("mssql", name) + ") IS NOT NULL BEGIN ALTER DATABASE " + qid("mssql", name) +
			" SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE " + qid("mssql", name) + " END")
	}
}

func dropLoginRaw(c testutil.Container, name string) {
	switch c.Engine {
	case "postgres":
		admin(c, "").exec("DROP ROLE IF EXISTS " + qid("postgres", name))
	default:
		admin(c, "").exec("IF EXISTS (SELECT 1 FROM sys.server_principals WHERE name = " + qstr("mssql", name) + ") DROP LOGIN " + qid("mssql", name))
	}
}

// mkLoginRaw crea un login con el admin crudo (permite nombres que la app rechazaría) y lo elimina al terminar.
func mkLoginRaw(t testing.TB, c testutil.Container, name, pw string) {
	t.Helper()
	var stmt string
	if c.Engine == "postgres" {
		stmt = "CREATE ROLE " + qid("postgres", name) + " LOGIN PASSWORD " + qstr("postgres", pw)
	} else {
		stmt = "CREATE LOGIN " + qid("mssql", name) + " WITH PASSWORD = " + qstr("mssql", pw)
	}
	if err := admin(c, "").exec(stmt); err != nil {
		t.Fatalf("crear login %s: %v", name, err)
	}
	t.Cleanup(func() { dropLoginRaw(c, name) })
}

func rowCount(t testing.TB, c testutil.Container, db string) int64 {
	t.Helper()
	n, err := admin(c, db).queryInt("SELECT COUNT(*) FROM t")
	if err != nil {
		t.Fatalf("contar filas de %s: %v", db, err)
	}
	return n
}

func dbExistsRaw(t testing.TB, c testutil.Container, name string) bool {
	t.Helper()
	var q string
	if c.Engine == "postgres" {
		q = "SELECT COUNT(*) FROM pg_database WHERE datname = " + qstr("postgres", name)
	} else {
		q = "SELECT COUNT(*) FROM sys.databases WHERE name = " + qstr("mssql", name)
	}
	n, err := admin(c, "").queryInt(q)
	if err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func loginExistsRaw(t testing.TB, c testutil.Container, name string) bool {
	t.Helper()
	var q string
	if c.Engine == "postgres" {
		q = "SELECT COUNT(*) FROM pg_roles WHERE rolname = " + qstr("postgres", name)
	} else {
		q = "SELECT COUNT(*) FROM sys.server_principals WHERE name = " + qstr("mssql", name)
	}
	n, err := admin(c, "").queryInt(q)
	if err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// levelOf busca la celda de la matriz.
func levelOf(m PermissionMatrix, login, db string) string {
	for _, c := range m.Cells {
		if c.Login == login && c.Database == db {
			return c.Level
		}
	}
	return "(sin celda)"
}

func hasDB(list []Database, name string) bool {
	for _, d := range list {
		if d.Name == name {
			return true
		}
	}
	return false
}

func hasLogin(list []Login, name string) bool {
	for _, l := range list {
		if l.Name == name {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// strongPW devuelve una contraseña válida para ambos motores.
func strongPW() string { return "Aa1!" + uniq("pw") }

// dockerExec ejecuta un comando en un contenedor de prueba y devuelve stdout+stderr.
func dockerExec(t testing.TB, name string, cmd ...string) (string, int) {
	t.Helper()
	cli := testutil.Docker(t)
	defer cli.Close()
	res, err := cli.Exec(context.Background(), name, dockerx.ExecOptions{Cmd: cmd})
	if err != nil {
		t.Fatalf("exec %v: %v", cmd, err)
	}
	return res.Stdout + res.Stderr, res.ExitCode
}

// waitUntil reintenta cond hasta el plazo.
func waitUntil(t testing.TB, d time.Duration, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("tiempo agotado esperando: %s", what)
}

func containerState(t testing.TB, name string) string {
	t.Helper()
	cli := testutil.Docker(t)
	defer cli.Close()
	info, err := cli.Raw().ContainerInspect(context.Background(), name)
	if err != nil {
		return "missing"
	}
	return info.State.Status
}

var _ = container.RemoveOptions{}

// openSession mantiene una sesión abierta como usuario en la base; devuelve la función que la cierra.
func openSession(t testing.TB, c testutil.Container, user, pw, db string) func() {
	t.Helper()
	ctx := context.Background()
	cn := c.Conn()
	cn.User, cn.Password = user, pw
	switch c.Engine {
	case "postgres":
		conn, err := pgx.Connect(ctx, postgres.ConnURL(cn, db))
		if err != nil {
			t.Fatalf("abrir sesión: %v", err)
		}
		if _, err := conn.Exec(ctx, "SELECT 1"); err != nil {
			t.Fatal(err)
		}
		return func() { conn.Close(ctx) }
	default:
		d, err := sql.Open("sqlserver", mssql.DSN(cn, db))
		if err != nil {
			t.Fatal(err)
		}
		d.SetMaxOpenConns(1)
		if _, err := d.ExecContext(ctx, "SELECT 1"); err != nil {
			t.Fatalf("abrir sesión: %v", err)
		}
		return func() { d.Close() }
	}
}
