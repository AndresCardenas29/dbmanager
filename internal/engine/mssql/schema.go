package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"dbmanager/internal/engine"
	"dbmanager/internal/validate"
)

// checkDB valida que la base exista y no sea del sistema.
func (e *Engine) checkDB(ctx context.Context, database string) error {
	if IsSystemDB(database) {
		return errors.New("no se pueden gestionar esquemas ni permisos en una base de datos del sistema")
	}
	if err := validate.Identifier(validate.EngineMSSQL, database); err != nil {
		return err
	}
	ex, err := e.DatabaseExists(ctx, database)
	if err != nil {
		return err
	}
	if !ex {
		return fmt.Errorf("la base de datos %q no existe", database)
	}
	return nil
}

func (e *Engine) schemaExists(ctx context.Context, database, schema string) (bool, error) {
	var n int
	err := e.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+validate.MSSQLIdent(database)+".sys.schemas WHERE name = @p1", schema).Scan(&n)
	return n > 0, err
}

// dbUser devuelve el usuario de la base asociado al login (Valid=false si no tiene).
func (e *Engine) dbUser(ctx context.Context, database, login string) (sql.NullString, error) {
	var user sql.NullString
	err := e.db.QueryRowContext(ctx, "SELECT dp.name FROM "+validate.MSSQLIdent(database)+".sys.database_principals dp JOIN sys.server_principals sp ON sp.sid = dp.sid WHERE sp.name = @p1 AND dp.type IN ('S','U','G')", login).Scan(&user)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return user, err
	}
	return user, nil
}

func (e *Engine) ListSchemas(ctx context.Context, database string) ([]engine.Schema, error) {
	if err := e.checkDB(ctx, database); err != nil {
		return nil, err
	}
	dq := validate.MSSQLIdent(database)
	rows, err := e.db.QueryContext(ctx, `SELECT s.name, ISNULL(p.name, '') FROM `+dq+`.sys.schemas s
LEFT JOIN `+dq+`.sys.database_principals p ON p.principal_id = s.principal_id
WHERE s.schema_id < 16384 AND s.name NOT IN ('sys', 'INFORMATION_SCHEMA', 'guest') ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	out := []engine.Schema{}
	idx := map[string]int{}
	for rows.Next() {
		var n, o string
		if err := rows.Scan(&n, &o); err != nil {
			rows.Close()
			return nil, err
		}
		idx[strings.ToLower(n)] = len(out)
		out = append(out, engine.Schema{Name: n, Owner: o, Tables: []string{}})
	}
	rows.Close()
	tr, err := e.db.QueryContext(ctx, `SELECT s.name, o.name FROM `+dq+`.sys.objects o JOIN `+dq+`.sys.schemas s ON s.schema_id = o.schema_id
WHERE o.type IN ('U','V') AND o.is_ms_shipped = 0 ORDER BY s.name, o.name`)
	if err != nil {
		return nil, err
	}
	defer tr.Close()
	for tr.Next() {
		var sn, tn string
		if err := tr.Scan(&sn, &tn); err != nil {
			return nil, err
		}
		if i, ok := idx[strings.ToLower(sn)]; ok {
			out[i].Tables = append(out[i].Tables, tn)
		}
	}
	return out, tr.Err()
}

func (e *Engine) CreateSchema(ctx context.Context, database, name string) error {
	if err := e.checkDB(ctx, database); err != nil {
		return err
	}
	if err := validate.Identifier(validate.EngineMSSQL, name); err != nil {
		return err
	}
	switch strings.ToLower(name) {
	case "sys", "information_schema", "guest":
		return errors.New("nombre de esquema reservado")
	}
	if ex, err := e.schemaExists(ctx, database, name); err != nil {
		return err
	} else if ex {
		return fmt.Errorf("el esquema %q ya existe", name)
	}
	_, err := e.db.ExecContext(ctx, inDB(database, "CREATE SCHEMA "+validate.MSSQLIdent(name)))
	return err
}

func (e *Engine) ListObjectGrants(ctx context.Context, database string) ([]engine.ObjectGrant, error) {
	if err := e.checkDB(ctx, database); err != nil {
		return nil, err
	}
	dq := validate.MSSQLIdent(database)
	rows, err := e.db.QueryContext(ctx, `SELECT sp.name, COALESCE(s.name, s2.name), ISNULL(ob.name, ''), dp.permission_name
FROM `+dq+`.sys.database_permissions dp
JOIN `+dq+`.sys.database_principals gp ON gp.principal_id = dp.grantee_principal_id
JOIN sys.server_principals sp ON sp.sid = gp.sid
LEFT JOIN `+dq+`.sys.schemas s ON dp.class = 3 AND s.schema_id = dp.major_id
LEFT JOIN `+dq+`.sys.objects ob ON dp.class = 1 AND dp.minor_id = 0 AND ob.object_id = dp.major_id AND ob.type IN ('U','V')
LEFT JOIN `+dq+`.sys.schemas s2 ON s2.schema_id = ob.schema_id
WHERE dp.state IN ('G','W') AND dp.class IN (1, 3)
 AND dp.permission_name IN ('SELECT','INSERT','UPDATE','DELETE','EXECUTE','ALTER')
 AND gp.type IN ('S','U','G') AND sp.type IN ('S','U','G')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type key struct{ l, s, t string }
	acc := map[key]map[string]bool{}
	for rows.Next() {
		var l, perm string
		var sc, t sql.NullString
		if err := rows.Scan(&l, &sc, &t, &perm); err != nil {
			return nil, err
		}
		if !sc.Valid || IsSystemLogin(l) {
			continue
		}
		if t.String != "" && perm == "EXECUTE" {
			continue
		}
		k := key{l, sc.String, t.String}
		if acc[k] == nil {
			acc[k] = map[string]bool{}
		}
		acc[k][perm] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []engine.ObjectGrant{}
	for k, set := range acc {
		g := engine.ObjectGrant{Login: k.l, Schema: k.s, Table: k.t, Privileges: []string{}}
		for _, p := range engine.Privileges {
			if set[p] {
				g.Privileges = append(g.Privileges, p)
			}
		}
		out = append(out, g)
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
	if err := validate.Identifier(validate.EngineMSSQL, login); err != nil {
		return err
	}
	if err := validate.Identifier(validate.EngineMSSQL, schema); err != nil {
		return err
	}
	if table != "" {
		if err := validate.Identifier(validate.EngineMSSQL, table); err != nil {
			return err
		}
	}
	if e.isSelf(login) {
		return errSelf
	}
	if IsSystemLogin(login) {
		return errors.New("no se puede modificar un login del sistema")
	}
	if err := e.checkDB(ctx, database); err != nil {
		return err
	}
	if ex, err := e.loginExists(ctx, login); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("el login %q no existe", login)
	}
	if ex, err := e.schemaExists(ctx, database, schema); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("el esquema %q no existe", schema)
	}
	dq := validate.MSSQLIdent(database)
	if table != "" {
		var n int
		if err := e.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+dq+".sys.objects o JOIN "+dq+".sys.schemas s ON s.schema_id = o.schema_id WHERE s.name = @p1 AND o.name = @p2 AND o.type IN ('U','V')", schema, table).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("la tabla %q no existe en el esquema %q", table, schema)
		}
	}
	user, err := e.dbUser(ctx, database, login)
	if err != nil {
		return err
	}
	uname := login
	switch {
	case user.Valid && user.String == "dbo":
		return errors.New("el login es el propietario de la base de datos (dbo) y ya tiene todos los permisos")
	case user.Valid:
		uname = user.String
	default:
		if _, err := e.db.ExecContext(ctx, inDB(database, "CREATE USER "+validate.MSSQLIdent(login)+" FOR LOGIN "+validate.MSSQLIdent(login))); err != nil {
			return err
		}
	}

	// Permisos gestionados actuales del usuario sobre este elemento.
	var curQ string
	args := []any{uname, schema}
	if table == "" {
		curQ = "SELECT dp.permission_name FROM " + dq + ".sys.database_permissions dp JOIN " + dq + ".sys.database_principals gp ON gp.principal_id = dp.grantee_principal_id " +
			"JOIN " + dq + ".sys.schemas s ON dp.class = 3 AND s.schema_id = dp.major_id WHERE gp.name = @p1 AND s.name = @p2 AND dp.state IN ('G','W')"
	} else {
		curQ = "SELECT dp.permission_name FROM " + dq + ".sys.database_permissions dp JOIN " + dq + ".sys.database_principals gp ON gp.principal_id = dp.grantee_principal_id " +
			"JOIN " + dq + ".sys.objects o ON dp.class = 1 AND dp.minor_id = 0 AND o.object_id = dp.major_id JOIN " + dq + ".sys.schemas s ON s.schema_id = o.schema_id " +
			"WHERE gp.name = @p1 AND s.name = @p2 AND o.name = @p3 AND dp.state IN ('G','W')"
		args = append(args, table)
	}
	rows, err := e.db.QueryContext(ctx, curQ, args...)
	if err != nil {
		return err
	}
	cur := map[string]bool{}
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			cur[p] = true
		}
	}
	rows.Close()

	securable := "SCHEMA::" + validate.MSSQLIdent(schema)
	if table != "" {
		securable = "OBJECT::" + validate.MSSQLIdent(schema) + "." + validate.MSSQLIdent(table)
	}
	want := map[string]bool{}
	for _, p := range privs {
		want[p] = true
	}
	uq := validate.MSSQLIdent(uname)
	for _, p := range engine.Privileges { // p proviene de la lista fija
		switch {
		case want[p] && !cur[p]:
			if _, err := e.db.ExecContext(ctx, inDB(database, "GRANT "+p+" ON "+securable+" TO "+uq)); err != nil {
				return err
			}
		case !want[p] && cur[p]:
			if _, err := e.db.ExecContext(ctx, inDB(database, "REVOKE "+p+" ON "+securable+" FROM "+uq)); err != nil {
				return err
			}
		}
	}
	if table == "" && want["ALTER"] {
		// Para crear objetos en el esquema también hace falta el permiso de base de datos correspondiente.
		if _, err := e.db.ExecContext(ctx, inDB(database, "GRANT CREATE TABLE, CREATE VIEW, CREATE PROCEDURE, CREATE FUNCTION TO "+uq)); err != nil {
			return err
		}
	}
	return nil
}
