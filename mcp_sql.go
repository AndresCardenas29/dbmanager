package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"dbmanager/internal/engine/mssql"
	"dbmanager/internal/engine/postgres"
	"dbmanager/internal/services"
	"dbmanager/internal/validate"

	"github.com/jackc/pgx/v5"
)

// sqlResult es el resultado de run_sql. Un error del motor (p. ej. permiso denegado) es un resultado, no un fallo de la herramienta.
type sqlResult struct {
	Ok           bool        `json:"ok"`
	Login        string      `json:"login"`
	Database     string      `json:"database"`
	Columns      []string    `json:"columns"`
	Rows         [][]*string `json:"rows"`
	RowsAffected int64       `json:"rows_affected"`
	Truncated    bool        `json:"truncated,omitempty"`
	Error        string      `json:"error,omitempty"`
}

var rePasswordLit = regexp.MustCompile(`(?i)(password\s*=?\s*)'[^']*'`)

// redactSQL recorta y oculta literales de contraseña para el registro de operaciones.
func redactSQL(s string) string {
	s = rePasswordLit.ReplaceAllString(strings.Join(strings.Fields(s), " "), "${1}'***'")
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120])
	}
	return s
}

// singleStatement rechaza varias sentencias (un ';' fuera de literales de texto, salvo el final).
func singleStatement(s string) (string, error) {
	s = strings.TrimSpace(s)
	for strings.HasSuffix(s, ";") {
		s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	}
	if s == "" {
		return "", errors.New("la sentencia SQL está vacía")
	}
	in := false
	for _, r := range s {
		switch {
		case r == '\'':
			in = !in
		case r == ';' && !in:
			return "", errors.New("solo se permite UNA sentencia por llamada")
		}
	}
	return s, nil
}

func returnsRows(stmt string) bool {
	f := strings.ToLower(strings.TrimLeft(stmt, " \t\r\n("))
	for _, k := range []string{"select", "with", "exec", "execute", "show", "explain", "values", "table", "call"} {
		if f == k || strings.HasPrefix(f, k+" ") || strings.HasPrefix(f, k+"\t") || strings.HasPrefix(f, k+"\n") || strings.HasPrefix(f, k+"\r") {
			return true
		}
	}
	return false
}

func toolRunSQL(ctx context.Context, c *mcpCall, in runSqlArgs) (any, error) {
	c.at(in.ServerID, fmt.Sprintf("%s as %s: %s", in.Database, in.Login, redactSQL(in.SQL)))
	if !c.set.AllowSql {
		return nil, errors.New("run_sql está desactivado. El usuario debe habilitar \"permitir SQL\" en Andres Tools > MCP")
	}
	stmt, err := singleStatement(in.SQL)
	if err != nil {
		return nil, err
	}
	st, err := c.a.db()
	if err != nil {
		return nil, err
	}
	sv, err := st.GetServer(in.ServerID)
	if err != nil {
		return nil, friendlyDBErr(err)
	}
	login := strings.TrimSpace(in.Login)
	if login == "" || strings.EqualFold(login, sv.AdminUser) {
		if c.set.Mode != mcpModeFull {
			return nil, errors.New(errMcpReadonly + " Ejecutar SQL como root requiere el modo completo; indique login para probar con los permisos de un usuario.")
		}
		login = sv.AdminUser
	}
	c.at(in.ServerID, fmt.Sprintf("%s as %s: %s", in.Database, login, redactSQL(in.SQL)))
	pw, err := c.a.GetLoginPassword(in.ServerID, login)
	if err != nil {
		return nil, err
	}
	if pw == "" {
		return nil, fmt.Errorf("no hay contraseña guardada para %q: use remember_login_password primero", login)
	}
	max := in.MaxRows
	if max <= 0 {
		max = 100
	}
	if max > 1000 {
		max = 1000
	}
	svc, err := c.a.services()
	if err != nil {
		return nil, err
	}
	conn := svc.ConnAs(sv, login, pw)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	res := sqlResult{Login: login, Database: in.Database, Columns: []string{}, Rows: [][]*string{}}
	var runErr error
	switch sv.Engine {
	case validate.EngineMSSQL:
		runErr = runMSSQL(cctx, mssql.DSN(conn, in.Database), stmt, max, &res)
	case validate.EnginePostgres:
		runErr = runPostgres(cctx, postgres.ConnURL(conn, in.Database), stmt, max, &res)
	default:
		return nil, errors.New("motor no soportado")
	}
	if runErr != nil {
		res.Ok = false
		res.Error = services.CleanErr(runErr)
		if res.Error == "" {
			res.Error = runErr.Error()
		}
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			res.Error = "tiempo de espera agotado (30 s)"
		}
		// Nunca devolver la contraseña si algún driver la incluyera en el mensaje.
		res.Error = strings.ReplaceAll(res.Error, pw, "********")
		return res, nil
	}
	res.Ok = true
	return res, nil
}

func cellString(v any) *string {
	if v == nil {
		return nil
	}
	var s string
	switch x := v.(type) {
	case []byte:
		s = string(x)
	case time.Time:
		s = x.Format(time.RFC3339Nano)
	case string:
		s = x
	default:
		s = fmt.Sprint(x)
	}
	return &s
}

func runMSSQL(ctx context.Context, dsn, stmt string, max int, res *sqlResult) error {
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if !returnsRows(stmt) {
		r, err := db.ExecContext(ctx, stmt)
		if err != nil {
			return err
		}
		res.RowsAffected, _ = r.RowsAffected()
		return nil
	}
	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	res.Columns = cols
	for rows.Next() {
		if len(res.Rows) >= max {
			res.Truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		row := make([]*string, len(cols))
		for i, v := range vals {
			row[i] = cellString(v)
		}
		res.Rows = append(res.Rows, row)
	}
	if !res.Truncated {
		if err := rows.Err(); err != nil {
			return err
		}
	}
	res.RowsAffected = int64(len(res.Rows))
	return nil
}

func runPostgres(ctx context.Context, url, stmt string, max int, res *sqlResult) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	// Protocolo simple: los resultados llegan en texto y se leen como cadenas sin importar el tipo.
	rows, err := conn.Query(ctx, stmt, pgx.QueryExecModeSimpleProtocol)
	if err != nil {
		return err
	}
	defer rows.Close()
	fds := rows.FieldDescriptions()
	cols := make([]string, len(fds))
	for i, f := range fds {
		cols[i] = f.Name
	}
	res.Columns = cols
	for rows.Next() {
		if len(res.Rows) >= max {
			res.Truncated = true
			break
		}
		vals := make([]*string, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		res.Rows = append(res.Rows, vals)
	}
	if res.Truncated {
		return nil
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	res.RowsAffected = rows.CommandTag().RowsAffected()
	return nil
}
