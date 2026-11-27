//go:build integration

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type itSQL struct {
	Ok           bool        `json:"ok"`
	Columns      []string    `json:"columns"`
	Rows         [][]*string `json:"rows"`
	RowsAffected int64       `json:"rows_affected"`
	Error        string      `json:"error"`
}

// TestMcpPermissionsFlow arma una base, un esquema y un usuario custom solo por MCP y verifica con run_sql
// (conectando COMO ese usuario) que SELECT funciona y que INSERT es denegado.
func TestMcpPermissionsFlow(t *testing.T) {
	for _, eng := range engines {
		eng := eng
		t.Run(eng, func(t *testing.T) {
			c := sharedContainer(t, eng)
			a, _ := newTestApp(t)
			sv := importShared(t, a, c)
			setMcp(t, a, McpSettings{Enabled: true, Mode: "full", AllowSql: true})
			cs := mcpClient(t, a)

			must := func(name string, args map[string]any) string {
				t.Helper()
				txt, isErr := mcpCallTool(t, cs, name, args)
				if isErr {
					t.Fatalf("%s: %s", name, txt)
				}
				return txt
			}
			db, user := uniq("mcpdb"), uniq("mcpu")
			t.Cleanup(func() { dropDBRaw(c, db); dropLoginRaw(c, user) })
			must("create_database", map[string]any{"server_id": sv.ID, "name": db})
			must("create_schema", map[string]any{"server_id": sv.ID, "database": db, "name": "pub"})
			must("create_schema", map[string]any{"server_id": sv.ID, "database": db, "name": "priv"})
			dbSQL := func(login, stmt string) itSQL {
				args := map[string]any{"server_id": sv.ID, "database": db, "sql": stmt}
				if login != "" {
					args["login"] = login
				}
				return itSQLCall(t, cs, args)
			}
			for _, s := range []string{
				"CREATE TABLE pub.items(id int, name varchar(20))",
				"INSERT INTO pub.items VALUES (1, 'a')",
				"CREATE TABLE priv.secrets(id int)",
			} {
				if r := dbSQL("", s); !r.Ok {
					t.Fatalf("root %q: %s", s, r.Error)
				}
			}
			must("create_database_user", map[string]any{"server_id": sv.ID, "database": db, "name": user, "password": strongPw, "level": "custom"})
			must("set_object_grant", map[string]any{"server_id": sv.ID, "database": db, "login": user, "schema": "pub", "privileges": []string{"SELECT"}})

			r := dbSQL(user, "SELECT id, name FROM pub.items")
			if !r.Ok || len(r.Rows) != 1 || r.Rows[0][1] == nil || *r.Rows[0][1] != "a" {
				t.Fatalf("SELECT como %s: %+v", user, r)
			}
			if r := dbSQL(user, "INSERT INTO pub.items VALUES (2, 'b')"); r.Ok || r.Error == "" {
				t.Fatalf("INSERT debería denegarse: %+v", r)
			} else if !strings.Contains(strings.ToLower(r.Error), "permission") && !strings.Contains(strings.ToLower(r.Error), "denied") {
				t.Errorf("mensaje inesperado: %s", r.Error)
			}
			if r := dbSQL(user, "SELECT * FROM priv.secrets"); r.Ok {
				t.Fatalf("priv.secrets no debería ser legible: %+v", r)
			}

			// readonly: run_sql con login sigue permitido; sin login (root) no
			setMcp(t, a, McpSettings{Enabled: true, Mode: "readonly", AllowSql: true})
			if r := dbSQL(user, "SELECT id FROM pub.items"); !r.Ok {
				t.Fatalf("run_sql con login en readonly: %+v", r)
			}
			if txt, isErr := mcpCallTool(t, cs, "run_sql", map[string]any{"server_id": sv.ID, "database": db, "sql": "SELECT 1"}); !isErr {
				t.Fatalf("run_sql root en readonly debe fallar: %s", txt)
			}
			// las contraseñas no se filtran en el historial
			ops, _ := a.ListOperations(200)
			for _, o := range ops {
				if strings.Contains(o.Target+o.Message, strongPw) {
					t.Errorf("operación con contraseña: %+v", o)
				}
			}
		})
	}
}

func itSQLCall(t *testing.T, cs *mcp.ClientSession, args map[string]any) itSQL {
	t.Helper()
	txt, isErr := mcpCallTool(t, cs, "run_sql", args)
	if isErr {
		t.Fatalf("run_sql falló como herramienta: %s", txt)
	}
	var r itSQL
	if err := json.Unmarshal([]byte(txt), &r); err != nil {
		t.Fatalf("respuesta: %v: %s", err, txt)
	}
	return r
}
