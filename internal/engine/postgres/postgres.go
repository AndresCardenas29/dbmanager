// Package postgres implementa engine.Engine para PostgreSQL (pgx/v5).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"dbmanager/internal/engine"
	"dbmanager/internal/validate"

	"github.com/jackc/pgx/v5"
)

// Engine es la implementación para PostgreSQL.
type Engine struct {
	c engine.Conn
}

var _ engine.Engine = (*Engine)(nil)

// New crea el engine (las conexiones se abren por operación).
func New(c engine.Conn) (*Engine, error) { return &Engine{c: c}, nil }

func (e *Engine) Close() error { return nil }

var systemDBs = map[string]bool{"template0": true, "template1": true, "postgres": true}

// IsSystemDB indica si es una base del sistema.
func IsSystemDB(name string) bool { return systemDBs[strings.ToLower(name)] }

// IsSystemRole indica si es un rol del sistema (pg_* o el superusuario postgres).
func IsSystemRole(name string) bool {
	return strings.HasPrefix(name, "pg_") || strings.EqualFold(name, "postgres")
}

// ConnURL construye la URL de conexión.
func ConnURL(c engine.Conn, database string) string {
	mode := "prefer"
	if c.TLS {
		mode = "verify-full"
		if c.TrustCert {
			mode = "require"
		}
	}
	q := url.Values{}
	q.Set("sslmode", mode)
	q.Set("connect_timeout", "10")
	q.Set("application_name", "DBManager")
	u := url.URL{Scheme: "postgres", User: url.UserPassword(c.User, c.Password),
		Host: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), Path: "/" + database, RawQuery: q.Encode()}
	return u.String()
}

func (e *Engine) connect(ctx context.Context, db string) (*pgx.Conn, error) {
	if db == "" {
		db = "postgres"
	}
	return pgx.Connect(ctx, ConnURL(e.c, db))
}

// ClassifyLevel convierte privilegios en un nivel de acceso.
func ClassifyLevel(isOwner, connect, canSelect, canWrite bool) string {
	switch {
	case isOwner:
		return engine.LevelOwner
	case !connect || !canSelect:
		return engine.LevelNone
	case canWrite:
		return engine.LevelReadWrite
	}
	return engine.LevelReadOnly
}

func (e *Engine) Ping(ctx context.Context) (string, error) {
	conn, err := e.connect(ctx, "postgres")
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)
	var v string
	if err := conn.QueryRow(ctx, "SHOW server_version").Scan(&v); err != nil {
		return "", err
	}
	return "PostgreSQL " + v, nil
}

func (e *Engine) ListDatabases(ctx context.Context) ([]engine.Database, error) {
	conn, err := e.connect(ctx, "postgres")
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `SELECT d.datname,
 CASE WHEN has_database_privilege(d.oid,'CONNECT') OR d.datname = current_database() THEN pg_database_size(d.oid) ELSE 0 END,
 pg_get_userbyid(d.datdba), CASE WHEN d.datallowconn THEN 'ONLINE' ELSE 'OFFLINE' END
 FROM pg_database d ORDER BY d.datname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []engine.Database{}
	for rows.Next() {
		var name, owner, state string
		var size int64
		if err := rows.Scan(&name, &size, &owner, &state); err != nil {
			return nil, err
		}
		out = append(out, engine.Database{Name: name, SizeMB: float64(int64(float64(size)/1048576*10+0.5)) / 10,
			Owner: owner, State: state, System: IsSystemDB(name)})
	}
	return out, rows.Err()
}

func (e *Engine) DatabaseExists(ctx context.Context, name string) (bool, error) {
	conn, err := e.connect(ctx, "postgres")
	if err != nil {
		return false, err
	}
	defer conn.Close(ctx)
	return dbExists(ctx, conn, name)
}

func dbExists(ctx context.Context, conn *pgx.Conn, name string) (bool, error) {
	var ok bool
	err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", name).Scan(&ok)
	return ok, err
}

func roleExists(ctx context.Context, conn *pgx.Conn, name string) (bool, error) {
	var ok bool
	err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", name).Scan(&ok)
	return ok, err
}

func (e *Engine) CreateDatabase(ctx context.Context, name, owner string) error {
	if err := validate.Identifier(validate.EnginePostgres, name); err != nil {
		return err
	}
	if IsSystemDB(name) {
		return errors.New("no se puede crear una base con el nombre de una base del sistema")
	}
	conn, err := e.connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if ex, err := dbExists(ctx, conn, name); err != nil {
		return err
	} else if ex {
		return fmt.Errorf("la base de datos %q ya existe", name)
	}
	q := "CREATE DATABASE " + validate.PGIdent(name)
	if owner != "" {
		if err := validate.Identifier(validate.EnginePostgres, owner); err != nil {
			return fmt.Errorf("propietario: %w", err)
		}
		if ex, err := roleExists(ctx, conn, owner); err != nil {
			return err
		} else if !ex {
			return fmt.Errorf("el rol %q no existe", owner)
		}
		q += " OWNER " + validate.PGIdent(owner)
	}
	_, err = conn.Exec(ctx, q)
	return err
}

func (e *Engine) DropDatabase(ctx context.Context, name string) error {
	if IsSystemDB(name) {
		return errors.New("no se puede eliminar una base de datos del sistema")
	}
	conn, err := e.connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if ex, err := dbExists(ctx, conn, name); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("la base de datos %q no existe", name)
	}
	_, err = conn.Exec(ctx, "DROP DATABASE "+validate.PGIdent(name)+" WITH (FORCE)")
	return err
}

func (e *Engine) ListLogins(ctx context.Context) ([]engine.Login, error) {
	conn, err := e.connect(ctx, "postgres")
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `SELECT rolname, rolsuper, rolcreatedb, rolcreaterole,
 (rolvaliduntil IS NOT NULL AND rolvaliduntil < now()) OR rolconnlimit = 0
 FROM pg_roles WHERE rolcanlogin ORDER BY rolname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []engine.Login{}
	for rows.Next() {
		var name string
		var su, cdb, crole, dis bool
		if err := rows.Scan(&name, &su, &cdb, &crole, &dis); err != nil {
			return nil, err
		}
		rs := []string{}
		if su {
			rs = append(rs, "SUPERUSER")
		}
		if cdb {
			rs = append(rs, "CREATEDB")
		}
		if crole {
			rs = append(rs, "CREATEROLE")
		}
		out = append(out, engine.Login{Name: name, ServerRoles: rs, Disabled: dis, IsSystem: IsSystemRole(name)})
	}
	return out, rows.Err()
}

// roleAttrs convierte la lista de roles en atributos (valida los permitidos).
func roleAttrs(roles []string) (su, cdb, crole bool, err error) {
	for _, r := range roles {
		switch strings.ToUpper(strings.TrimSpace(r)) {
		case "SUPERUSER":
			su = true
		case "CREATEDB":
			cdb = true
		case "CREATEROLE":
			crole = true
		case "":
		default:
			return false, false, false, fmt.Errorf("rol de servidor no permitido: %s", r)
		}
	}
	return
}

func attrSQL(su, cdb, crole bool) string {
	f := func(b bool, w string) string {
		if b {
			return w
		}
		return "NO" + w
	}
	return f(su, "SUPERUSER") + " " + f(cdb, "CREATEDB") + " " + f(crole, "CREATEROLE")
}

func (e *Engine) CreateLogin(ctx context.Context, name, password string, roles []string) error {
	if err := validate.Identifier(validate.EnginePostgres, name); err != nil {
		return err
	}
	if IsSystemRole(name) {
		return errors.New("nombre de rol reservado")
	}
	if password == "" {
		return errors.New("la contraseña es obligatoria")
	}
	su, cdb, crole, err := roleAttrs(roles)
	if err != nil {
		return err
	}
	conn, err := e.connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if ex, err := roleExists(ctx, conn, name); err != nil {
		return err
	} else if ex {
		return fmt.Errorf("el usuario %q ya existe", name)
	}
	_, err = conn.Exec(ctx, "CREATE ROLE "+validate.PGIdent(name)+" LOGIN "+attrSQL(su, cdb, crole)+" PASSWORD "+validate.PGString(password))
	return err
}

func (e *Engine) DropLogin(ctx context.Context, name string) error {
	if e.isSelf(name) {
		return errSelf
	}
	if IsSystemRole(name) {
		return errors.New("no se puede eliminar un rol del sistema")
	}
	conn, err := e.connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if ex, err := roleExists(ctx, conn, name); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("el usuario %q no existe", name)
	}
	var owned []string
	rows, err := conn.Query(ctx, "SELECT datname FROM pg_database WHERE datdba = (SELECT oid FROM pg_roles WHERE rolname=$1)", name)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d string
		rows.Scan(&d)
		owned = append(owned, d)
	}
	rows.Close()
	if len(owned) > 0 {
		return fmt.Errorf("el usuario %q es propietario de las bases: %s; cambie el propietario antes de eliminarlo", name, strings.Join(owned, ", "))
	}
	// Revoca privilegios otorgados a este rol en cada base para poder eliminarlo.
	if dbs, err := e.matrixDatabases(ctx, conn); err == nil {
		for _, d := range dbs {
			if err := e.revokeAll(ctx, d, name); err != nil {
				return fmt.Errorf("no se pudieron revocar los privilegios en %s: %w", d, err)
			}
		}
	}
	_, err = conn.Exec(ctx, "DROP ROLE "+validate.PGIdent(name))
	if err != nil {
		return fmt.Errorf("%w (si el usuario es propietario de objetos, reasigne esos objetos antes de eliminarlo)", err)
	}
	return nil
}

func (e *Engine) ChangePassword(ctx context.Context, name, password string) error {
	if e.isSelf(name) {
		return errSelf
	}
	if IsSystemRole(name) {
		return errors.New("no se puede modificar un rol del sistema")
	}
	if password == "" {
		return errors.New("la contraseña es obligatoria")
	}
	conn, err := e.connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if ex, err := roleExists(ctx, conn, name); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("el usuario %q no existe", name)
	}
	_, err = conn.Exec(ctx, "ALTER ROLE "+validate.PGIdent(name)+" WITH PASSWORD "+validate.PGString(password))
	return err
}

func (e *Engine) SetServerRoles(ctx context.Context, name string, roles []string) error {
	if e.isSelf(name) {
		return errSelf
	}
	if IsSystemRole(name) {
		return errors.New("no se puede modificar un rol del sistema")
	}
	su, cdb, crole, err := roleAttrs(roles)
	if err != nil {
		return err
	}
	conn, err := e.connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if ex, err := roleExists(ctx, conn, name); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("el usuario %q no existe", name)
	}
	_, err = conn.Exec(ctx, "ALTER ROLE "+validate.PGIdent(name)+" WITH "+attrSQL(su, cdb, crole))
	return err
}

func (e *Engine) matrixDatabases(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	rows, err := conn.Query(ctx, "SELECT datname FROM pg_database WHERE NOT datistemplate AND datallowconn AND datname <> 'postgres' ORDER BY datname")
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
		out = append(out, n)
	}
	return out, rows.Err()
}

const matrixQuery = `
SELECT r.rolname,
 has_database_privilege(r.oid, current_database(), 'CONNECT'),
 (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind IN ('r','p')),
 (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind IN ('r','p') AND has_table_privilege(r.oid, c.oid, 'SELECT')),
 (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind IN ('r','p')
   AND has_table_privilege(r.oid, c.oid, 'INSERT') AND has_table_privilege(r.oid, c.oid, 'UPDATE') AND has_table_privilege(r.oid, c.oid, 'DELETE')),
 EXISTS(SELECT 1 FROM pg_default_acl d, aclexplode(d.defaclacl) a WHERE d.defaclnamespace = 'public'::regnamespace AND d.defaclobjtype = 'r' AND a.grantee = r.oid AND a.privilege_type = 'SELECT'),
 EXISTS(SELECT 1 FROM pg_default_acl d, aclexplode(d.defaclacl) a WHERE d.defaclnamespace = 'public'::regnamespace AND d.defaclobjtype = 'r' AND a.grantee = r.oid AND a.privilege_type = 'INSERT')
FROM pg_roles r WHERE r.rolcanlogin AND r.rolname NOT LIKE 'pg\_%' AND r.rolname <> 'postgres'`

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
	admin, err := e.connect(ctx, "postgres")
	if err != nil {
		return m, err
	}
	defer admin.Close(ctx)
	dbs, err := e.matrixDatabases(ctx, admin)
	if err != nil {
		return m, err
	}
	m.Databases = append(m.Databases, dbs...)
	owners := map[string]string{}
	if rows, err := admin.Query(ctx, "SELECT datname, pg_get_userbyid(datdba) FROM pg_database"); err == nil {
		for rows.Next() {
			var d, o string
			if rows.Scan(&d, &o) == nil {
				owners[d] = o
			}
		}
		rows.Close()
	}
	type key struct{ l, d string }
	levels := map[key]string{}
	for _, d := range dbs {
		conn, err := e.connect(ctx, d)
		if err != nil {
			continue
		}
		rows, err := conn.Query(ctx, matrixQuery)
		if err == nil {
			for rows.Next() {
				var l string
				var cn, defSel, defIns bool
				var ntab, nsel, nwr int64
				if rows.Scan(&l, &cn, &ntab, &nsel, &nwr, &defSel, &defIns) != nil {
					continue
				}
				sel, wr := defSel, defIns
				if ntab > 0 {
					sel, wr = nsel == ntab, nwr == ntab
				}
				levels[key{l, d}] = ClassifyLevel(owners[d] == l, cn, sel, wr)
			}
			rows.Close()
		}
		conn.Close(ctx)
	}
	for _, l := range m.Logins {
		for _, d := range dbs {
			lv := levels[key{l, d}]
			if lv == "" {
				lv = ClassifyLevel(owners[d] == l, false, false, false)
			}
			m.Cells = append(m.Cells, engine.Cell{Login: l, Database: d, Level: lv})
		}
	}
	return m, nil
}

// revokeSQL devuelve las sentencias (dentro de la base) que quitan los privilegios de un rol.
func revokeSQL(role string) []string {
	r := validate.PGIdent(role)
	return []string{
		"REVOKE ALL ON ALL TABLES IN SCHEMA public FROM " + r,
		"REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM " + r,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM " + r,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM " + r,
		"REVOKE ALL ON SCHEMA public FROM " + r,
	}
}

// grantSQL devuelve las sentencias (dentro de la base) para readonly / readwrite / owner.
func grantSQL(role, level string) []string {
	r := validate.PGIdent(role)
	switch level {
	case engine.LevelOwner:
		return []string{
			"GRANT ALL ON SCHEMA public TO " + r,
			"GRANT ALL ON ALL TABLES IN SCHEMA public TO " + r,
			"GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO " + r,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO " + r,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO " + r,
		}
	case engine.LevelReadWrite:
		return []string{
			"GRANT USAGE ON SCHEMA public TO " + r,
			"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + r,
			"GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO " + r,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + r,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO " + r,
		}
	case engine.LevelReadOnly:
		return []string{
			"GRANT USAGE ON SCHEMA public TO " + r,
			"GRANT SELECT ON ALL TABLES IN SCHEMA public TO " + r,
			"GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO " + r,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO " + r,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON SEQUENCES TO " + r,
		}
	}
	return nil
}

func execAll(ctx context.Context, conn *pgx.Conn, stmts []string) error {
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// revokeAll quita todos los privilegios del rol en la base (esquema public y CONNECT).
func (e *Engine) revokeAll(ctx context.Context, database, role string, forRoles ...string) error {
	conn, err := e.connect(ctx, database)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if err := execAll(ctx, conn, revokeSQL(role)); err != nil {
		return err
	}
	if len(forRoles) == 0 { // p. ej. al eliminar un usuario: también limpia los privilegios por defecto del propietario
		var o string
		if conn.QueryRow(ctx, "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()").Scan(&o) == nil && o != role && o != e.c.User {
			forRoles = []string{o}
		}
	}
	bestEffort(ctx, conn, forRoleSQL(role, "", forRoles, true))
	_, err = conn.Exec(ctx, "REVOKE ALL ON DATABASE "+validate.PGIdent(database)+" FROM "+validate.PGIdent(role))
	return err
}

func (e *Engine) SetDatabaseAccess(ctx context.Context, login, database, level string) error {
	if !engine.ValidLevel(level) {
		return fmt.Errorf("nivel de acceso inválido: %s", level)
	}
	if IsSystemRole(login) {
		return errors.New("no se puede modificar un rol del sistema")
	}
	if IsSystemDB(database) {
		return errors.New("no se pueden cambiar permisos en una base de datos del sistema")
	}
	admin, err := e.connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer admin.Close(ctx)
	if ex, err := roleExists(ctx, admin, login); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("el usuario %q no existe", login)
	}
	if ex, err := dbExists(ctx, admin, database); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("la base de datos %q no existe", database)
	}
	var owner string
	if err := admin.QueryRow(ctx, "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname=$1", database).Scan(&owner); err != nil {
		return err
	}
	dbq, lq := validate.PGIdent(database), validate.PGIdent(login)
	if err := restrictPublicConnect(ctx, admin, database, owner, login, level); err != nil {
		return err
	}

	if level == engine.LevelOwner {
		if owner != login {
			if _, err := admin.Exec(ctx, "ALTER DATABASE "+dbq+" OWNER TO "+lq); err != nil {
				return err
			}
			// ALTER OWNER traslada las entradas de la ACL del propietario anterior al nuevo: se conserva su CONNECT.
			if _, err := admin.Exec(ctx, "GRANT CONNECT ON DATABASE "+dbq+" TO "+validate.PGIdent(owner)); err != nil {
				return err
			}
		}
		conn, err := e.connect(ctx, database)
		if err != nil {
			return err
		}
		defer conn.Close(ctx)
		return execAll(ctx, conn, grantSQL(login, level))
	}
	if owner == login {
		// Al bajar de propietario, la propiedad vuelve al usuario administrador.
		if _, err := admin.Exec(ctx, "ALTER DATABASE "+dbq+" OWNER TO "+validate.PGIdent(e.c.User)); err != nil {
			return err
		}
	}
	var effOwners []string
	if owner != login && owner != e.c.User {
		effOwners = append(effOwners, owner)
	}
	if err := e.revokeAll(ctx, database, login, effOwners...); err != nil {
		return err
	}
	if level == engine.LevelNone {
		return nil
	}
	if _, err := admin.Exec(ctx, "GRANT CONNECT ON DATABASE "+dbq+" TO "+lq); err != nil {
		return err
	}
	conn, err := e.connect(ctx, database)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if err := execAll(ctx, conn, grantSQL(login, level)); err != nil {
		return err
	}
	bestEffort(ctx, conn, forRoleSQL(login, level, effOwners, false))
	return nil
}

// RenameDatabase renombra una base cortando antes las conexiones abiertas a ambas.
func (e *Engine) RenameDatabase(ctx context.Context, from, to string) error {
	if IsSystemDB(from) || IsSystemDB(to) {
		return errors.New("no se pueden renombrar bases del sistema")
	}
	conn, err := e.connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = ANY($1) AND pid <> pg_backend_pid()", []string{from, to}); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, "ALTER DATABASE "+validate.PGIdent(from)+" RENAME TO "+validate.PGIdent(to))
	return err
}

var errSelf = errors.New("no se puede modificar, cambiar la contraseña ni eliminar el usuario con el que la app está conectada a este servidor (podría bloquear el servidor)")

// isSelf indica si name es el login con el que se conecta la app.
func (e *Engine) isSelf(name string) bool { return name == e.c.User }

func bestEffort(ctx context.Context, conn *pgx.Conn, stmts []string) {
	for _, s := range stmts {
		conn.Exec(ctx, s) // mejor esfuerzo: requiere privilegios sobre el rol "FOR ROLE"
	}
}

// forRoleSQL genera ALTER DEFAULT PRIVILEGES FOR ROLE <r> para que también cubra las tablas que cree
// el propietario de la base (no solo el admin). revoke=true quita todo lo dado al rol.
func forRoleSQL(role, level string, forRoles []string, revoke bool) []string {
	var out []string
	lr := validate.PGIdent(role)
	for _, f := range forRoles {
		fr := validate.PGIdent(f)
		pre := "ALTER DEFAULT PRIVILEGES FOR ROLE " + fr + " IN SCHEMA public "
		if revoke {
			out = append(out, pre+"REVOKE ALL ON TABLES FROM "+lr, pre+"REVOKE ALL ON SEQUENCES FROM "+lr)
			continue
		}
		switch level {
		case engine.LevelReadWrite:
			out = append(out, pre+"GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "+lr, pre+"GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO "+lr)
		case engine.LevelReadOnly:
			out = append(out, pre+"GRANT SELECT ON TABLES TO "+lr, pre+"GRANT SELECT ON SEQUENCES TO "+lr)
		}
	}
	return out
}

// restrictPublicConnect quita CONNECT a PUBLIC sin dejar fuera a nadie: antes lo concede explícitamente
// al propietario y a cada rol de usuario que hoy puede conectarse (salvo el destino cuando el nivel es none).
func restrictPublicConnect(ctx context.Context, admin *pgx.Conn, database, owner, target, level string) error {
	var pub bool
	err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database d, aclexplode(COALESCE(d.datacl, acldefault('d', d.datdba))) a
 WHERE d.datname = $1 AND a.grantee = 0 AND a.privilege_type = 'CONNECT')`, database).Scan(&pub)
	if err != nil {
		return err
	}
	if !pub {
		return nil
	}
	rows, err := admin.Query(ctx, `SELECT rolname FROM pg_roles WHERE rolcanlogin AND rolname NOT LIKE 'pg\_%' AND rolname <> 'postgres'
 AND has_database_privilege(oid, $1, 'CONNECT')`, database)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil && !(n == target && level == engine.LevelNone) {
			names = append(names, n)
		}
	}
	rows.Close()
	if owner != "" {
		names = append(names, owner)
	}
	dbq := validate.PGIdent(database)
	for _, n := range names {
		if _, err := admin.Exec(ctx, "GRANT CONNECT ON DATABASE "+dbq+" TO "+validate.PGIdent(n)); err != nil {
			return err
		}
	}
	_, err = admin.Exec(ctx, "REVOKE CONNECT ON DATABASE "+dbq+" FROM PUBLIC")
	return err
}
