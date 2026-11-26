package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"dbmanager/internal/engine"
	"dbmanager/internal/validate"

	"github.com/golang-sql/sqlexp"
)

var percentRe = regexp.MustCompile(`(\d+)\s+percent\s+(processed|backed up)`)

// ParsePercent extrae "N percent processed" de un mensaje informativo de SQL Server.
func ParsePercent(msg string) (int, bool) {
	m := percentRe.FindStringSubmatch(msg)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

// execMessages ejecuta un batch capturando los mensajes informativos (STATS) y errores.
func execMessages(ctx context.Context, conn *sql.Conn, q string, onMsg func(string)) error {
	ret := &sqlexp.ReturnMessage{}
	rows, err := conn.QueryContext(ctx, q, ret)
	if err != nil {
		return err
	}
	defer rows.Close()
	var first error
	for active := true; active; {
		switch m := ret.Message(ctx).(type) {
		case sqlexp.MsgNotice:
			if onMsg != nil && m.Message != nil {
				onMsg(m.Message.String())
			}
		case sqlexp.MsgNext:
			for rows.Next() {
			}
		case sqlexp.MsgError:
			if first == nil {
				first = m.Error
			}
		case sqlexp.MsgNextResultSet:
			active = rows.NextResultSet()
		}
	}
	if first != nil {
		return first
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return rows.Err()
}

// BackupDatabase ejecuta BACKUP DATABASE hacia una ruta del servidor.
func (e *Engine) BackupDatabase(ctx context.Context, database, serverPath string, progress engine.ProgressFunc) error {
	if ex, err := e.DatabaseExists(ctx, database); err != nil {
		return err
	} else if !ex {
		return fmt.Errorf("la base de datos %q no existe", database)
	}
	conn, err := e.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	q := "BACKUP DATABASE " + validate.MSSQLIdent(database) + " TO DISK = " + validate.MSSQLString(serverPath) + " WITH INIT, STATS = 10"
	return execMessages(ctx, conn, q, func(msg string) {
		if p, ok := ParsePercent(msg); ok && progress != nil {
			progress(p, msg)
		}
	})
}

func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

// queryMaps ejecuta una consulta y devuelve cada fila como mapa columna->valor.
func (e *Engine) queryMaps(ctx context.Context, q string) ([]map[string]any, error) {
	rows, err := e.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InspectFile lee los archivos lógicos y el nombre de la base de un .bak ubicado en el servidor.
func (e *Engine) InspectFile(ctx context.Context, serverPath string) (engine.BackupInfo, error) {
	info := engine.BackupInfo{Format: "mssql-bak", Files: []engine.BackupFile{}}
	lit := validate.MSSQLString(serverPath)
	hdr, err := e.queryMaps(ctx, "RESTORE HEADERONLY FROM DISK = "+lit)
	if err != nil {
		return info, fmt.Errorf("no se pudo leer el backup: %w", err)
	}
	if len(hdr) > 0 {
		info.DatabaseName = asString(hdr[0]["DatabaseName"])
	}
	fl, err := e.queryMaps(ctx, "RESTORE FILELISTONLY FROM DISK = "+lit)
	if err != nil {
		return info, fmt.Errorf("no se pudo leer la lista de archivos del backup: %w", err)
	}
	for _, r := range fl {
		t := "data"
		switch strings.ToUpper(asString(r["Type"])) {
		case "L":
			t = "log"
		case "D":
		default:
			continue // fulltext/filestream no soportados
		}
		info.Files = append(info.Files, engine.BackupFile{LogicalName: asString(r["LogicalName"]),
			PhysicalName: asString(r["PhysicalName"]), Type: t})
	}
	return info, nil
}

// DefaultPaths devuelve las carpetas de datos y log por defecto del servidor.
func (e *Engine) DefaultPaths(ctx context.Context) (data, log string) {
	var d, l sql.NullString
	e.db.QueryRowContext(ctx, `SELECT CAST(SERVERPROPERTY('InstanceDefaultDataPath') AS NVARCHAR(512)), CAST(SERVERPROPERTY('InstanceDefaultLogPath') AS NVARCHAR(512))`).Scan(&d, &l)
	data, log = strings.TrimRight(d.String, `/\`), strings.TrimRight(l.String, `/\`)
	if data == "" {
		data = "/var/opt/mssql/data"
	}
	if log == "" {
		log = data
	}
	return
}

// Move es un MOVE de RESTORE.
type Move struct {
	Logical  string
	Physical string
}

func joinPath(dir, name string) string {
	dir = strings.TrimRight(dir, `/\`)
	sep := "/"
	if strings.Contains(dir, `\`) || (len(dir) >= 2 && dir[1] == ':') {
		sep = `\`
	}
	return dir + sep + name
}

// PlanMoves genera nombres físicos únicos basados en la base destino.
func PlanMoves(files []engine.BackupFile, target, dataDir, logDir string) []Move {
	target = fileBase(target)
	var out []Move
	d, l := 0, 0
	for _, f := range files {
		var name, dir string
		if f.Type == "log" {
			l++
			dir = logDir
			if l == 1 {
				name = target + "_log.ldf"
			} else {
				name = fmt.Sprintf("%s_log_%d.ldf", target, l)
			}
		} else {
			d++
			dir = dataDir
			if d == 1 {
				name = target + ".mdf"
			} else {
				name = fmt.Sprintf("%s_%d.ndf", target, d)
			}
		}
		out = append(out, Move{Logical: f.LogicalName, Physical: joinPath(dir, name)})
	}
	return out
}

// RestoreOptions parámetros de RestoreDatabase.
type RestoreOptions struct {
	ServerPath string
	Target     string
	DataDir    string // "" = valor por defecto del servidor
	LogDir     string
	Replace    bool
}

// BuildRestoreSQL construye la sentencia RESTORE.
func BuildRestoreSQL(serverPath, target string, moves []Move, replace bool) string {
	var sb strings.Builder
	sb.WriteString("RESTORE DATABASE " + validate.MSSQLIdent(target) + " FROM DISK = " + validate.MSSQLString(serverPath) + " WITH ")
	for _, m := range moves {
		sb.WriteString("MOVE " + validate.MSSQLString(m.Logical) + " TO " + validate.MSSQLString(m.Physical) + ", ")
	}
	if replace {
		sb.WriteString("REPLACE, ")
	}
	sb.WriteString("STATS = 5")
	return sb.String()
}

// RestoreDatabase restaura un .bak (ruta del servidor) en la base destino.
func (e *Engine) RestoreDatabase(ctx context.Context, o RestoreOptions, progress engine.ProgressFunc) error {
	if err := validate.Identifier(validate.EngineMSSQL, o.Target); err != nil {
		return err
	}
	if IsSystemDB(o.Target) {
		return errors.New("no se puede restaurar sobre una base de datos del sistema")
	}
	exists, err := e.DatabaseExists(ctx, o.Target)
	if err != nil {
		return err
	}
	if exists && !o.Replace {
		return fmt.Errorf("la base de datos %q ya existe; elija otro nombre o active \"reemplazar\"", o.Target)
	}
	info, err := e.InspectFile(ctx, o.ServerPath)
	if err != nil {
		return err
	}
	if len(info.Files) == 0 {
		return errors.New("el backup no contiene archivos de datos")
	}
	dd, ld := o.DataDir, o.LogDir
	defD, defL := e.DefaultPaths(ctx)
	if dd == "" {
		dd = defD
	}
	if ld == "" {
		ld = defL
		if o.DataDir != "" {
			ld = o.DataDir
		}
	}
	q := BuildRestoreSQL(o.ServerPath, o.Target, PlanMoves(info.Files, o.Target, dd, ld), o.Replace)

	conn, err := e.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	id := validate.MSSQLIdent(o.Target)
	if exists {
		conn.ExecContext(ctx, "ALTER DATABASE "+id+" SET SINGLE_USER WITH ROLLBACK IMMEDIATE")
	}
	err = execMessages(ctx, conn, q, func(msg string) {
		if p, ok := ParsePercent(msg); ok && progress != nil {
			progress(p, msg)
		}
	})
	if err != nil && exists {
		conn.ExecContext(context.Background(), "ALTER DATABASE "+id+" SET MULTI_USER")
	}
	return err
}
