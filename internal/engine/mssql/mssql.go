// Package mssql implementa engine.Engine para SQL Server (go-mssqldb).
package mssql

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"dbmanager/internal/engine"
	"dbmanager/internal/validate"

	_ "github.com/microsoft/go-mssqldb"
)

// Engine es la implementación para SQL Server.
type Engine struct {
	c  engine.Conn
	db *sql.DB
}

var _ engine.Engine = (*Engine)(nil)

var systemDBs = map[string]bool{"master": true, "tempdb": true, "model": true, "msdb": true}

// Roles de servidor permitidos.
var serverRoles = []string{"sysadmin", "serveradmin", "securityadmin", "processadmin", "setupadmin", "bulkadmin", "diskadmin", "dbcreator"}

// DSN construye la cadena de conexión.
func DSN(c engine.Conn, database string) string {
	q := url.Values{}
	if database != "" {
		q.Set("database", database)
	}
	if c.TLS {
		q.Set("encrypt", "true")
	} else {
		q.Set("encrypt", "false")
	}
	if c.TrustCert {
		q.Set("TrustServerCertificate", "true")
	}
	q.Set("connection timeout", "10")
	q.Set("app name", "DBManager")
	u := url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		RawQuery: q.Encode(),
	}
	return u.String()
}

// New abre una conexión (perezosa) al servidor.
func New(c engine.Conn) (*Engine, error) {
	db, err := sql.Open("sqlserver", DSN(c, "master"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &Engine{c: c, db: db}, nil
}

func (e *Engine) Close() error { return e.db.Close() }

// IsSystemDB indica si es una base de datos del sistema.
func IsSystemDB(name string) bool { return systemDBs[strings.ToLower(name)] }

// IsSystemLogin indica si es un login del sistema (sa, ##x##, NT ...).
func IsSystemLogin(name string) bool {
	return strings.EqualFold(name, "sa") || strings.HasPrefix(name, "##") || strings.HasPrefix(strings.ToUpper(name), "NT ")
}

// ClassifyLevel convierte pertenencias a roles en un nivel de acceso.
func ClassifyLevel(isOwner, reader, writer bool) string {
	switch {
	case isOwner:
		return engine.LevelOwner
	case reader && writer:
		return engine.LevelReadWrite
	case reader:
		return engine.LevelReadOnly
	}
	return engine.LevelNone
}

// RolesForLevel devuelve los roles de base de datos que corresponden a un nivel.
func RolesForLevel(level string) []string {
	switch level {
	case engine.LevelOwner:
		return []string{"db_owner"}
	case engine.LevelReadWrite:
		return []string{"db_datareader", "db_datawriter"}
	case engine.LevelReadOnly:
		return []string{"db_datareader"}
	}
	return nil
}

var managedDBRoles = []string{"db_owner", "db_datareader", "db_datawriter"}

// inDB envuelve una sentencia para ejecutarla en el contexto de otra base de datos.
func inDB(db, stmt string) string {
	return "EXEC " + validate.MSSQLIdent(db) + ".sys.sp_executesql " + validate.MSSQLString(stmt)
}

func (e *Engine) Ping(ctx context.Context) (string, error) {
	var v, ed sql.NullString
	err := e.db.QueryRowContext(ctx, `SELECT CAST(SERVERPROPERTY('ProductVersion') AS NVARCHAR(64)), CAST(SERVERPROPERTY('Edition') AS NVARCHAR(128))`).Scan(&v, &ed)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace("SQL Server " + v.String + " (" + ed.String + ")"), nil
}

func (e *Engine) ListDatabases(ctx context.Context) ([]engine.Database, error) {
	rows, err := e.db.QueryContext(ctx, `
SELECT d.name, CAST(SUM(CAST(mf.size AS BIGINT)) * 8.0 / 1024 AS FLOAT), SUSER_SNAME(d.owner_sid), d.state_desc
FROM sys.databases d
LEFT JOIN sys.master_files mf ON mf.database_id = d.database_id
GROUP BY d.name, d.owner_sid, d.state_desc
ORDER BY d.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []engine.Database{}
	for rows.Next() {
		var name string
		var size sql.NullFloat64
		var owner, state sql.NullString
		if err := rows.Scan(&name, &size, &owner, &state); err != nil {
			return nil, err
		}
		out = append(out, engine.Database{Name: name, SizeMB: round1(size.Float64), Owner: owner.String,
			State: state.String, System: IsSystemDB(name)})
	}
	return out, rows.Err()
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

func (e *Engine) DatabaseExists(ctx context.Context, name string) (bool, error) {
	var n int
	err := e.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys.databases WHERE name = @p1`, name).Scan(&n)
	return n > 0, err
}

func (e *Engine) loginExists(ctx context.Context, name string) (bool, error) {
	var n int
	err := e.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys.server_principals WHERE name = @p1 AND type IN ('S','U','G')`, name).Scan(&n)
	return n > 0, err
}

func (e *Engine) CreateDatabase(ctx context.Context, name, owner string) error {
	if err := validate.Identifier(validate.EngineMSSQL, name); err != nil {
		return err
	}
	if IsSystemDB(name) {
		return errors.New("no se puede crear una base con el nombre de una base del sistema")
	}
	if ex, err := e.DatabaseExists(ctx, name); err != nil {
		return err
	} else if ex {
		return fmt.Errorf("la base de datos %q ya existe", name)
	}
	if owner != "" {
		if err := validate.Identifier(validate.EngineMSSQL, owner); err != nil {
			return fmt.Errorf("propietario: %w", err)
		}
		if ex, err := e.loginExists(ctx, owner); err != nil {
			return err
		} else if !ex {
			return fmt.Errorf("el login %q no existe", owner)
		}
	}
	if _, err := e.db.ExecContext(ctx, e.createDatabaseSQL(ctx, name)); err != nil {
		return err
	}
	if owner != "" {
		if _, err := e.db.ExecContext(ctx, "ALTER AUTHORIZATION ON DATABASE::"+validate.MSSQLIdent(name)+" TO "+validate.MSSQLIdent(owner)); err != nil {
			return fmt.Errorf("la base se creó pero no se pudo asignar el propietario: %w", err)
		}
	}
	return nil
}

func (e *Engine) DropDatabase(ctx context.Context, name string) error {
	if IsSystemDB(name) {
		return errors.New("no se puede eliminar una base de datos del sistema")
	}
	if ex, err := e.DatabaseExists(ctx, name); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("la base de datos %q no existe", name)
	}
	conn, err := e.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	id := validate.MSSQLIdent(name)
	// Si está offline no admite SET SINGLE_USER; se ignora el error y se intenta el DROP.
	conn.ExecContext(ctx, "ALTER DATABASE "+id+" SET SINGLE_USER WITH ROLLBACK IMMEDIATE")
	if _, err := conn.ExecContext(ctx, "DROP DATABASE "+id); err != nil {
		conn.ExecContext(ctx, "ALTER DATABASE "+id+" SET MULTI_USER")
		return err
	}
	return nil
}

func (e *Engine) ListLogins(ctx context.Context) ([]engine.Login, error) {
	roles := map[string][]string{}
	rr, err := e.db.QueryContext(ctx, `
SELECT m.name, r.name FROM sys.server_role_members rm
JOIN sys.server_principals r ON r.principal_id = rm.role_principal_id
JOIN sys.server_principals m ON m.principal_id = rm.member_principal_id
ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	for rr.Next() {
		var m, r string
		if err := rr.Scan(&m, &r); err != nil {
			rr.Close()
			return nil, err
		}
		roles[m] = append(roles[m], r)
	}
	rr.Close()

	rows, err := e.db.QueryContext(ctx, `SELECT name, is_disabled FROM sys.server_principals WHERE type IN ('S','U','G') ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []engine.Login{}
	for rows.Next() {
		var name string
		var dis bool
		if err := rows.Scan(&name, &dis); err != nil {
			return nil, err
		}
		rs := roles[name]
		if rs == nil {
			rs = []string{}
		}
		out = append(out, engine.Login{Name: name, ServerRoles: rs, Disabled: dis, IsSystem: IsSystemLogin(name)})
	}
	return out, rows.Err()
}

func checkRoles(roles []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, r := range roles {
		r = strings.ToLower(strings.TrimSpace(r))
		if r == "" || seen[r] {
			continue
		}
		ok := false
		for _, a := range serverRoles {
			if a == r {
				ok = true
			}
		}
		if !ok {
			return nil, fmt.Errorf("rol de servidor no permitido: %s", r)
		}
		seen[r] = true
		out = append(out, r)
	}
	return out, nil
}

func (e *Engine) CreateLogin(ctx context.Context, name, password string, roles []string) error {
	if err := validate.Identifier(validate.EngineMSSQL, name); err != nil {
		return err
	}
	if IsSystemLogin(name) {
		return errors.New("nombre de login reservado")
	}
	if err := validate.SQLServerPassword(password); err != nil {
		return err
	}
	rs, err := checkRoles(roles)
	if err != nil {
		return err
	}
	if ex, err := e.loginExists(ctx, name); err != nil {
		return err
	} else if ex {
		return fmt.Errorf("el login %q ya existe", name)
	}
	if _, err := e.db.ExecContext(ctx, "CREATE LOGIN "+validate.MSSQLIdent(name)+" WITH PASSWORD = "+validate.MSSQLString(password)); err != nil {
		return err
	}
	for _, r := range rs {
		if _, err := e.db.ExecContext(ctx, "ALTER SERVER ROLE "+validate.MSSQLIdent(r)+" ADD MEMBER "+validate.MSSQLIdent(name)); err != nil {
			return fmt.Errorf("el login se creó pero falló al asignar el rol %s: %w", r, err)
		}
	}
	return nil
}

func (e *Engine) userDatabases(ctx context.Context) ([]string, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT name FROM sys.databases WHERE state = 0 AND database_id > 4 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if !IsSystemDB(n) {
			out = append(out, n)
		}
	}
	return out, rows.Err()
}

func (e *Engine) DropLogin(ctx context.Context, name string) error {
	if e.isSelf(name) {
		return errSelf
	}
	if IsSystemLogin(name) {
		return errors.New("no se puede eliminar un login del sistema")
	}
	if ex, err := e.loginExists(ctx, name); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("el login %q no existe", name)
	}
	// Elimina los usuarios asociados en cada base para no dejar usuarios huérfanos (mejor esfuerzo).
	if dbs, err := e.userDatabases(ctx); err == nil {
		for _, d := range dbs {
			var u sql.NullString
			err := e.db.QueryRowContext(ctx, "SELECT dp.name FROM "+validate.MSSQLIdent(d)+".sys.database_principals dp JOIN sys.server_principals sp ON sp.sid = dp.sid WHERE sp.name = @p1 AND dp.type IN ('S','U','G') AND dp.name <> 'dbo'", name).Scan(&u)
			if err == nil && u.Valid {
				e.db.ExecContext(ctx, inDB(d, "DROP USER "+validate.MSSQLIdent(u.String)))
			}
		}
	}
	// Cierra sesiones abiertas del login.
	rows, err := e.db.QueryContext(ctx, `SELECT session_id FROM sys.dm_exec_sessions WHERE login_name = @p1 AND session_id <> @@SPID`, name)
	if err == nil {
		var ids []int
		for rows.Next() {
			var id int
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			e.db.ExecContext(ctx, "KILL "+strconv.Itoa(id))
		}
	}
	_, err = e.db.ExecContext(ctx, "DROP LOGIN "+validate.MSSQLIdent(name))
	return err
}

func (e *Engine) ChangePassword(ctx context.Context, name, password string) error {
	if e.isSelf(name) {
		return errSelf
	}
	if IsSystemLogin(name) {
		return errors.New("no se puede modificar un login del sistema")
	}
	if err := validate.SQLServerPassword(password); err != nil {
		return err
	}
	if ex, err := e.loginExists(ctx, name); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("el login %q no existe", name)
	}
	_, err := e.db.ExecContext(ctx, "ALTER LOGIN "+validate.MSSQLIdent(name)+" WITH PASSWORD = "+validate.MSSQLString(password))
	return err
}

func (e *Engine) SetServerRoles(ctx context.Context, name string, roles []string) error {
	if e.isSelf(name) {
		return errSelf
	}
	if IsSystemLogin(name) {
		return errors.New("no se puede modificar un login del sistema")
	}
	want, err := checkRoles(roles)
	if err != nil {
		return err
	}
	if ex, err := e.loginExists(ctx, name); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("el login %q no existe", name)
	}
	cur := map[string]bool{}
	rows, err := e.db.QueryContext(ctx, `SELECT r.name FROM sys.server_role_members rm
JOIN sys.server_principals r ON r.principal_id = rm.role_principal_id
JOIN sys.server_principals m ON m.principal_id = rm.member_principal_id WHERE m.name = @p1`, name)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r string
		rows.Scan(&r)
		cur[r] = true
	}
	rows.Close()
	wantSet := map[string]bool{}
	for _, r := range want {
		wantSet[r] = true
		if !cur[r] {
			if _, err := e.db.ExecContext(ctx, "ALTER SERVER ROLE "+validate.MSSQLIdent(r)+" ADD MEMBER "+validate.MSSQLIdent(name)); err != nil {
				return err
			}
		}
	}
	for _, r := range serverRoles {
		if cur[r] && !wantSet[r] {
			if _, err := e.db.ExecContext(ctx, "ALTER SERVER ROLE "+validate.MSSQLIdent(r)+" DROP MEMBER "+validate.MSSQLIdent(name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) GetPermissionMatrix(ctx context.Context) (engine.Matrix, error) {
	m := engine.Matrix{Logins: []string{}, Databases: []string{}, Cells: []engine.Cell{}}
	logins, err := e.ListLogins(ctx)
	if err != nil {
		return m, err
	}
	for _, l := range logins {
		if !l.IsSystem {
			m.Logins = append(m.Logins, l.Name)
		}
	}
	dbs, err := e.userDatabases(ctx)
	if err != nil {
		return m, err
	}
	m.Databases = append(m.Databases, dbs...)

	owners := map[string]string{} // db -> login propietario
	if rows, err := e.db.QueryContext(ctx, `SELECT d.name, sp.name FROM sys.databases d JOIN sys.server_principals sp ON sp.sid = d.owner_sid`); err == nil {
		for rows.Next() {
			var d, l string
			if rows.Scan(&d, &l) == nil {
				owners[d] = l
			}
		}
		rows.Close()
	}

	type key struct{ l, d string }
	levels := map[key]string{}
	for _, d := range dbs {
		q := `SELECT sp.name,
 MAX(CASE WHEN r.name = 'db_owner' THEN 1 ELSE 0 END),
 MAX(CASE WHEN r.name = 'db_datareader' THEN 1 ELSE 0 END),
 MAX(CASE WHEN r.name = 'db_datawriter' THEN 1 ELSE 0 END)
FROM ` + validate.MSSQLIdent(d) + `.sys.database_principals dp
JOIN sys.server_principals sp ON sp.sid = dp.sid
LEFT JOIN ` + validate.MSSQLIdent(d) + `.sys.database_role_members rm ON rm.member_principal_id = dp.principal_id
LEFT JOIN ` + validate.MSSQLIdent(d) + `.sys.database_principals r ON r.principal_id = rm.role_principal_id
WHERE dp.type IN ('S','U','G') AND sp.type IN ('S','U','G')
GROUP BY sp.name`
		rows, err := e.db.QueryContext(ctx, q)
		if err != nil {
			continue // base inaccesible: se muestra como "none"
		}
		for rows.Next() {
			var l string
			var o, r, w int
			if rows.Scan(&l, &o, &r, &w) == nil {
				levels[key{l, d}] = ClassifyLevel(o == 1 || owners[d] == l, r == 1, w == 1)
			}
		}
		rows.Close()
	}
	for _, l := range m.Logins {
		for _, d := range dbs {
			lv := levels[key{l, d}]
			if lv == "" {
				lv = ClassifyLevel(owners[d] == l, false, false)
			}
			m.Cells = append(m.Cells, engine.Cell{Login: l, Database: d, Level: lv})
		}
	}
	return m, nil
}

func (e *Engine) SetDatabaseAccess(ctx context.Context, login, database, level string) error {
	if !engine.ValidLevel(level) {
		return fmt.Errorf("nivel de acceso inválido: %s", level)
	}
	if IsSystemLogin(login) {
		return errors.New("no se puede modificar un login del sistema")
	}
	if IsSystemDB(database) {
		return errors.New("no se pueden cambiar permisos en una base de datos del sistema")
	}
	if ex, err := e.loginExists(ctx, login); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("el login %q no existe", login)
	}
	if ex, err := e.DatabaseExists(ctx, database); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("la base de datos %q no existe", database)
	}
	dbq := validate.MSSQLIdent(database)

	var user sql.NullString
	err := e.db.QueryRowContext(ctx, "SELECT dp.name FROM "+dbq+".sys.database_principals dp JOIN sys.server_principals sp ON sp.sid = dp.sid WHERE sp.name = @p1 AND dp.type IN ('S','U','G')", login).Scan(&user)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if user.Valid && user.String == "dbo" {
		if level == engine.LevelOwner {
			return nil
		}
		return errors.New("el login es el propietario de la base de datos (dbo); cambie el propietario antes de modificar su acceso")
	}
	if level == engine.LevelNone {
		if user.Valid {
			if _, err := e.db.ExecContext(ctx, inDB(database, "DROP USER "+validate.MSSQLIdent(user.String))); err != nil {
				return err
			}
		}
		return nil
	}
	uname := login
	if !user.Valid {
		if _, err := e.db.ExecContext(ctx, inDB(database, "CREATE USER "+validate.MSSQLIdent(login)+" FOR LOGIN "+validate.MSSQLIdent(login))); err != nil {
			return err
		}
	} else {
		uname = user.String
	}
	cur := map[string]bool{}
	rows, err := e.db.QueryContext(ctx, "SELECT r.name FROM "+dbq+".sys.database_role_members rm JOIN "+dbq+".sys.database_principals r ON r.principal_id = rm.role_principal_id JOIN "+dbq+".sys.database_principals m ON m.principal_id = rm.member_principal_id WHERE m.name = @p1", uname)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r string
		rows.Scan(&r)
		cur[r] = true
	}
	rows.Close()
	want := map[string]bool{}
	for _, r := range RolesForLevel(level) {
		want[r] = true
	}
	for _, r := range managedDBRoles {
		switch {
		case want[r] && !cur[r]:
			if _, err := e.db.ExecContext(ctx, inDB(database, "ALTER ROLE "+validate.MSSQLIdent(r)+" ADD MEMBER "+validate.MSSQLIdent(uname))); err != nil {
				return err
			}
		case !want[r] && cur[r]:
			if _, err := e.db.ExecContext(ctx, inDB(database, "ALTER ROLE "+validate.MSSQLIdent(r)+" DROP MEMBER "+validate.MSSQLIdent(uname))); err != nil {
				return err
			}
		}
	}
	return nil
}

var errSelf = errors.New("no se puede modificar, cambiar la contraseña ni eliminar el login con el que la app está conectada a este servidor (podría bloquear el servidor)")

func (e *Engine) isSelf(name string) bool { return strings.EqualFold(name, e.c.User) }

// longNameLimit: por encima de este largo los archivos físicos usan nombres cortos, porque
// SQL Server falla con "<nombre>_log.ldf" demasiado largo.
const longNameLimit = 100

// fileBase devuelve el nombre base de los archivos físicos: el propio nombre, o uno corto con hash si es largo.
func fileBase(name string) string {
	if len(name) <= longNameLimit {
		return name
	}
	h := sha1.Sum([]byte(name))
	return "db_" + hex.EncodeToString(h[:8])
}

func (e *Engine) createDatabaseSQL(ctx context.Context, name string) string {
	q := "CREATE DATABASE " + validate.MSSQLIdent(name)
	if len(name) <= longNameLimit {
		return q
	}
	data, log := e.DefaultPaths(ctx)
	b := fileBase(name)
	return q + " ON PRIMARY (NAME = " + validate.MSSQLString(b) + ", FILENAME = " + validate.MSSQLString(joinPath(data, b+".mdf")) +
		") LOG ON (NAME = " + validate.MSSQLString(b+"_log") + ", FILENAME = " + validate.MSSQLString(joinPath(log, b+"_log.ldf")) + ")"
}
