package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"dbmanager/internal/engine"
	"dbmanager/internal/validate"

	"github.com/jackc/pgx/v5"
)

// En PostgreSQL no existen permisos "sobre todas las tablas de un esquema" como en SQL Server. Un privilegio
// a nivel de esquema se modela así:
//   - SELECT/INSERT/UPDATE/DELETE: el rol tiene el privilegio EXPLÍCITO en todas las tablas del esquema
//     (o, si el esquema no tiene tablas, en pg_default_acl para tablas);
//   - EXECUTE: igual con las funciones (o pg_default_acl para funciones);
//   - ALTER: el rol tiene CREATE sobre el esquema.
//
// Los privilegios de tabla se informan solo cuando no están ya cubiertos por el nivel de esquema.
// (Un esquema con una sola tabla no distingue ambos niveles: se informa como nivel de esquema.)

const sysSchemaFilter = `n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'`
const loginRoleFilter = `g.rolcanlogin AND g.rolname NOT LIKE 'pg\_%' AND g.rolname <> 'postgres'`
const notExtFunc = `NOT EXISTS (SELECT 1 FROM pg_depend dd WHERE dd.classid = 'pg_proc'::regclass AND dd.objid = p.oid AND dd.deptype = 'e')`

var tablePrivs = []string{"SELECT", "INSERT", "UPDATE", "DELETE"}

// checkDB valida la base y la abre como admin. El llamador cierra la conexión.
func (e *Engine) openDB(ctx context.Context, database string) (*pgx.Conn, error) {
	if IsSystemDB(database) {
		return nil, errors.New("no se pueden gestionar esquemas ni permisos en una base de datos del sistema")
	}
	if err := validate.Identifier(validate.EnginePostgres, database); err != nil {
		return nil, err
	}
	admin, err := e.connect(ctx, "postgres")
	if err != nil {
		return nil, err
	}
	ex, err := dbExists(ctx, admin, database)
	admin.Close(ctx)
	if err != nil {
		return nil, err
	}
	if !ex {
		return nil, fmt.Errorf("la base de datos %q no existe", database)
	}
	return e.connect(ctx, database)
}

func listSchemas(ctx context.Context, conn *pgx.Conn) ([]engine.Schema, error) {
	rows, err := conn.Query(ctx, `SELECT n.nspname, pg_get_userbyid(n.nspowner),
 COALESCE(array_agg(c.relname::text ORDER BY c.relname::text) FILTER (WHERE c.relname IS NOT NULL), '{}'::text[])
FROM pg_namespace n
LEFT JOIN pg_class c ON c.relnamespace = n.oid AND c.relkind IN ('r','p','v','m') AND NOT c.relispartition
WHERE `+sysSchemaFilter+`
GROUP BY n.oid, n.nspname, n.nspowner ORDER BY n.nspname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []engine.Schema{}
	for rows.Next() {
		var s engine.Schema
		if err := rows.Scan(&s.Name, &s.Owner, &s.Tables); err != nil {
			return nil, err
		}
		if s.Tables == nil {
			s.Tables = []string{}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (e *Engine) ListSchemas(ctx context.Context, database string) ([]engine.Schema, error) {
	conn, err := e.openDB(ctx, database)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	return listSchemas(ctx, conn)
}

func (e *Engine) CreateSchema(ctx context.Context, database, name string) error {
	if err := validate.Identifier(validate.EnginePostgres, name); err != nil {
		return err
	}
	l := strings.ToLower(name)
	if strings.HasPrefix(l, "pg_") || l == "information_schema" {
		return errors.New("nombre de esquema reservado")
	}
	conn, err := e.openDB(ctx, database)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	var ex bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = $1)", name).Scan(&ex); err != nil {
		return err
	}
	if ex {
		return fmt.Errorf("el esquema %q ya existe", name)
	}
	_, err = conn.Exec(ctx, "CREATE SCHEMA "+validate.PGIdent(name))
	return err
}

type schemaKey struct{ role, schema string }
type tableKey struct{ role, schema, table string }

type loadedGrants struct {
	schemas []engine.Schema
	schema  map[schemaKey]map[string]bool
	table   map[tableKey]map[string]bool // solo lo no cubierto por el nivel de esquema
}

func add(m map[string]bool, k string) map[string]bool {
	if m == nil {
		m = map[string]bool{}
	}
	m[k] = true
	return m
}

func loadGrants(ctx context.Context, conn *pgx.Conn) (*loadedGrants, error) {
	schemas, err := listSchemas(ctx, conn)
	if err != nil {
		return nil, err
	}
	lg := &loadedGrants{schemas: schemas, schema: map[schemaKey]map[string]bool{}, table: map[tableKey]map[string]bool{}}

	// Privilegios explícitos por tabla.
	tabExpl := map[tableKey]map[string]bool{}
	rows, err := conn.Query(ctx, `SELECT n.nspname, c.relname::text, g.rolname::text, a.privilege_type
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
CROSS JOIN LATERAL aclexplode(c.relacl) a JOIN pg_roles g ON g.oid = a.grantee
WHERE c.relkind IN ('r','p','v','m') AND NOT c.relispartition AND `+sysSchemaFilter+` AND `+loginRoleFilter+`
 AND a.privilege_type IN ('SELECT','INSERT','UPDATE','DELETE')`)
	if err != nil {
		return nil, err
	}
	roles := map[schemaKey]bool{}
	for rows.Next() {
		var s, t, r, p string
		if err := rows.Scan(&s, &t, &r, &p); err != nil {
			rows.Close()
			return nil, err
		}
		k := tableKey{r, s, t}
		tabExpl[k] = add(tabExpl[k], p)
		roles[schemaKey{r, s}] = true
	}
	rows.Close()

	// EXECUTE explícito por función y total de funciones por esquema.
	funcExpl := map[schemaKey]int{}
	rows, err = conn.Query(ctx, `SELECT n.nspname, g.rolname::text, count(*)
FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
CROSS JOIN LATERAL aclexplode(p.proacl) a JOIN pg_roles g ON g.oid = a.grantee
WHERE p.prokind IN ('f','p') AND a.privilege_type = 'EXECUTE' AND `+sysSchemaFilter+` AND `+loginRoleFilter+` AND `+notExtFunc+`
GROUP BY n.nspname, g.rolname`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s, r string
		var n int
		if err := rows.Scan(&s, &r, &n); err != nil {
			rows.Close()
			return nil, err
		}
		funcExpl[schemaKey{r, s}] = n
		roles[schemaKey{r, s}] = true
	}
	rows.Close()
	funcTotal := map[string]int{}
	rows, err = conn.Query(ctx, `SELECT n.nspname, count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
WHERE p.prokind IN ('f','p') AND `+sysSchemaFilter+` AND `+notExtFunc+` GROUP BY n.nspname`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			rows.Close()
			return nil, err
		}
		funcTotal[s] = n
	}
	rows.Close()

	// Privilegios por defecto (para esquemas sin tablas / funciones).
	defTab := map[schemaKey]map[string]bool{}
	defFunc := map[schemaKey]bool{}
	rows, err = conn.Query(ctx, `SELECT n.nspname, g.rolname::text, d.defaclobjtype::text, a.privilege_type
FROM pg_default_acl d JOIN pg_namespace n ON n.oid = d.defaclnamespace
CROSS JOIN LATERAL aclexplode(d.defaclacl) a JOIN pg_roles g ON g.oid = a.grantee
WHERE d.defaclobjtype IN ('r','f') AND `+sysSchemaFilter+` AND `+loginRoleFilter)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s, r, ot, p string
		if err := rows.Scan(&s, &r, &ot, &p); err != nil {
			rows.Close()
			return nil, err
		}
		k := schemaKey{r, s}
		if ot == "r" {
			defTab[k] = add(defTab[k], p)
		} else if p == "EXECUTE" {
			defFunc[k] = true
		}
		roles[k] = true
	}
	rows.Close()

	// CREATE sobre el esquema => ALTER.
	create := map[schemaKey]bool{}
	rows, err = conn.Query(ctx, `SELECT n.nspname, g.rolname::text FROM pg_namespace n
CROSS JOIN LATERAL aclexplode(n.nspacl) a JOIN pg_roles g ON g.oid = a.grantee
WHERE a.privilege_type = 'CREATE' AND `+sysSchemaFilter+` AND `+loginRoleFilter)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s, r string
		if err := rows.Scan(&s, &r); err != nil {
			rows.Close()
			return nil, err
		}
		create[schemaKey{r, s}] = true
		roles[schemaKey{r, s}] = true
	}
	rows.Close()

	for k := range roles {
		var tables []string
		for _, s := range schemas {
			if s.Name == k.schema {
				tables = s.Tables
			}
		}
		set := map[string]bool{}
		if len(tables) > 0 {
			for _, p := range tablePrivs {
				all := true
				for _, t := range tables {
					if !tabExpl[tableKey{k.role, k.schema, t}][p] {
						all = false
						break
					}
				}
				if all {
					set[p] = true
				}
			}
		} else {
			for p := range defTab[k] {
				for _, tp := range tablePrivs {
					if p == tp {
						set[p] = true
					}
				}
			}
		}
		if ft := funcTotal[k.schema]; ft > 0 {
			if funcExpl[k] >= ft {
				set["EXECUTE"] = true
			}
		} else if defFunc[k] {
			set["EXECUTE"] = true
		}
		if create[k] {
			set["ALTER"] = true
		}
		if len(set) > 0 {
			lg.schema[k] = set
		}
		for _, t := range tables {
			tk := tableKey{k.role, k.schema, t}
			rest := map[string]bool{}
			for p := range tabExpl[tk] {
				if !set[p] {
					rest[p] = true
				}
			}
			if len(rest) > 0 {
				lg.table[tk] = rest
			}
		}
	}
	return lg, nil
}

func orderedPrivs(set map[string]bool) []string {
	out := []string{}
	for _, p := range engine.Privileges {
		if set[p] {
			out = append(out, p)
		}
	}
	return out
}

func (e *Engine) ListObjectGrants(ctx context.Context, database string) ([]engine.ObjectGrant, error) {
	conn, err := e.openDB(ctx, database)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	lg, err := loadGrants(ctx, conn)
	if err != nil {
		return nil, err
	}
	out := []engine.ObjectGrant{}
	for k, set := range lg.schema {
		if ps := orderedPrivs(set); len(ps) > 0 {
			out = append(out, engine.ObjectGrant{Login: k.role, Schema: k.schema, Privileges: ps})
		}
	}
	for k, set := range lg.table {
		if ps := orderedPrivs(set); len(ps) > 0 {
			out = append(out, engine.ObjectGrant{Login: k.role, Schema: k.schema, Table: k.table, Privileges: ps})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Login != b.Login {
			return a.Login < b.Login
		}
		if a.Schema != b.Schema {
			return a.Schema < b.Schema
		}
		return a.Table < b.Table
	})
	return out, nil
}

func (e *Engine) SetObjectGrant(ctx context.Context, database, login, schema, table string, privileges []string) error {
	privs, err := engine.NormalizePrivileges(privileges, table != "")
	if err != nil {
		return err
	}
	for _, v := range []string{login, schema} {
		if err := validate.Identifier(validate.EnginePostgres, v); err != nil {
			return err
		}
	}
	if table != "" {
		if err := validate.Identifier(validate.EnginePostgres, table); err != nil {
			return err
		}
		for _, p := range privs {
			if p == "ALTER" {
				return errors.New("ALTER solo se puede otorgar a nivel de esquema en PostgreSQL, no sobre una tabla")
			}
		}
	}
	if e.isSelf(login) {
		return errSelf
	}
	if IsSystemRole(login) {
		return errors.New("no se puede modificar un rol del sistema")
	}
	admin, err := e.connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer admin.Close(ctx)
	if IsSystemDB(database) {
		return errors.New("no se pueden cambiar permisos en una base de datos del sistema")
	}
	if ex, err := dbExists(ctx, admin, database); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("la base de datos %q no existe", database)
	}
	if ex, err := roleExists(ctx, admin, login); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("el usuario %q no existe", login)
	}
	var owner string
	if err := admin.QueryRow(ctx, "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname=$1", database).Scan(&owner); err != nil {
		return err
	}
	if owner == login {
		return errors.New("el usuario es propietario de la base de datos y ya tiene todos los permisos")
	}
	dbq, lq, sq := validate.PGIdent(database), validate.PGIdent(login), validate.PGIdent(schema)

	conn, err := e.connect(ctx, database)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	var schemaOwner string
	if err := conn.QueryRow(ctx, "SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname = $1", schema).Scan(&schemaOwner); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("el esquema %q no existe", schema)
		}
		return err
	}
	if table != "" {
		var ok bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r','p','v','m'))`, schema, table).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("la tabla %q no existe en el esquema %q", table, schema)
		}
	}
	lg, err := loadGrants(ctx, conn)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, p := range privs {
		want[p] = true
	}

	if len(privs) > 0 {
		// El rol necesita poder conectarse explícitamente (PUBLIC pierde CONNECT).
		if err := restrictPublicConnect(ctx, admin, database, owner, login, engine.LevelCustom); err != nil {
			return err
		}
		if _, err := admin.Exec(ctx, "GRANT CONNECT ON DATABASE "+dbq+" TO "+lq); err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, "GRANT USAGE ON SCHEMA "+sq+" TO "+lq); err != nil {
			return err
		}
	}

	otherTable := false // ¿quedan permisos de tabla (distintos de esta tabla) en el esquema?
	for k := range lg.table {
		if k.role == login && k.schema == schema && k.table != table {
			otherTable = true
		}
	}
	anyTableGrant := otherTable
	if table != "" {
		cur := lg.table[tableKey{login, schema, table}]
		tq := sq + "." + validate.PGIdent(table)
		for _, p := range tablePrivs {
			switch {
			case want[p]:
				if _, err := conn.Exec(ctx, "GRANT "+p+" ON TABLE "+tq+" TO "+lq); err != nil {
					return err
				}
			case cur[p]:
				if _, err := conn.Exec(ctx, "REVOKE "+p+" ON TABLE "+tq+" FROM "+lq); err != nil {
					return err
				}
			}
		}
		// Nota: no se otorgan secuencias de la tabla (mejor esfuerzo omitido); un INSERT sobre tablas con
		// columnas serial requiere además USAGE en la secuencia (se concede con el permiso de esquema).
		anyTableGrant = anyTableGrant || len(privs) > 0
		if len(lg.schema[schemaKey{login, schema}]) == 0 && !anyTableGrant {
			conn.Exec(ctx, "REVOKE USAGE ON SCHEMA "+sq+" FROM "+lq)
		}
		return nil
	}

	// Nivel de esquema. Quitar un privilegio que hoy está a nivel de esquema lo quita de TODAS las tablas del
	// esquema (incluidos permisos de tabla equivalentes); los permisos de tabla no cubiertos se conservan.
	cur := lg.schema[schemaKey{login, schema}]
	var defOwners []string
	if schemaOwner != e.c.User {
		defOwners = append(defOwners, schemaOwner)
	}
	defaults := func(verb, what, to string, priv string) {
		stmt := "ALTER DEFAULT PRIVILEGES IN SCHEMA " + sq + " " + verb + " " + priv + " ON " + what + " " + to + " " + lq
		conn.Exec(ctx, stmt) // mejor esfuerzo para no fallar en esquemas sin permisos de default
		for _, o := range defOwners {
			conn.Exec(ctx, "ALTER DEFAULT PRIVILEGES FOR ROLE "+validate.PGIdent(o)+" IN SCHEMA "+sq+" "+verb+" "+priv+" ON "+what+" "+to+" "+lq)
		}
	}
	for _, p := range tablePrivs {
		if want[p] {
			if _, err := conn.Exec(ctx, "GRANT "+p+" ON ALL TABLES IN SCHEMA "+sq+" TO "+lq); err != nil {
				return err
			}
			defaults("GRANT", "TABLES", "TO", p)
			continue
		}
		if cur[p] {
			if _, err := conn.Exec(ctx, "REVOKE "+p+" ON ALL TABLES IN SCHEMA "+sq+" FROM "+lq); err != nil {
				return err
			}
		}
		defaults("REVOKE", "TABLES", "FROM", p)
	}
	if want["INSERT"] || want["UPDATE"] {
		if _, err := conn.Exec(ctx, "GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA "+sq+" TO "+lq); err != nil {
			return err
		}
		defaults("GRANT", "SEQUENCES", "TO", "USAGE, SELECT")
	} else {
		if (cur["INSERT"] || cur["UPDATE"]) && !anyTableGrant {
			if _, err := conn.Exec(ctx, "REVOKE ALL ON ALL SEQUENCES IN SCHEMA "+sq+" FROM "+lq); err != nil {
				return err
			}
		}
		defaults("REVOKE", "SEQUENCES", "FROM", "ALL")
	}
	if want["EXECUTE"] {
		if _, err := conn.Exec(ctx, "GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA "+sq+" TO "+lq); err != nil {
			return err
		}
		defaults("GRANT", "FUNCTIONS", "TO", "EXECUTE")
	} else {
		if cur["EXECUTE"] {
			if _, err := conn.Exec(ctx, "REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA "+sq+" FROM "+lq); err != nil {
				return err
			}
		}
		defaults("REVOKE", "FUNCTIONS", "FROM", "EXECUTE")
	}
	if want["ALTER"] {
		if _, err := conn.Exec(ctx, "GRANT CREATE ON SCHEMA "+sq+" TO "+lq); err != nil {
			return err
		}
	} else if cur["ALTER"] {
		if _, err := conn.Exec(ctx, "REVOKE CREATE ON SCHEMA "+sq+" FROM "+lq); err != nil {
			return err
		}
	}
	if len(privs) == 0 && !anyTableGrant {
		if _, err := conn.Exec(ctx, "REVOKE USAGE ON SCHEMA "+sq+" FROM "+lq); err != nil {
			return err
		}
	}
	return nil
}

// revokeOtherSchemas quita (mejor esfuerzo) los privilegios del rol en los esquemas de usuario distintos de public.
func revokeOtherSchemas(ctx context.Context, conn *pgx.Conn, role string, forRoles []string) {
	rows, err := conn.Query(ctx, `SELECT n.nspname FROM pg_namespace n WHERE `+sysSchemaFilter+` AND n.nspname <> 'public'`)
	if err != nil {
		return
	}
	var schemas []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			schemas = append(schemas, s)
		}
	}
	rows.Close()
	r := validate.PGIdent(role)
	for _, s := range schemas {
		sq := validate.PGIdent(s)
		stmts := []string{
			"REVOKE ALL ON ALL TABLES IN SCHEMA " + sq + " FROM " + r,
			"REVOKE ALL ON ALL SEQUENCES IN SCHEMA " + sq + " FROM " + r,
			"REVOKE ALL ON ALL FUNCTIONS IN SCHEMA " + sq + " FROM " + r,
			"REVOKE ALL ON SCHEMA " + sq + " FROM " + r,
		}
		for _, what := range []string{"TABLES", "SEQUENCES", "FUNCTIONS"} {
			stmts = append(stmts, "ALTER DEFAULT PRIVILEGES IN SCHEMA "+sq+" REVOKE ALL ON "+what+" FROM "+r)
			for _, f := range forRoles {
				stmts = append(stmts, "ALTER DEFAULT PRIVILEGES FOR ROLE "+validate.PGIdent(f)+" IN SCHEMA "+sq+" REVOKE ALL ON "+what+" FROM "+r)
			}
		}
		bestEffort(ctx, conn, stmts)
	}
}
