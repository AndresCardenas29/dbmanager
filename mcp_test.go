package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpClient conecta un cliente MCP en memoria a un servidor construido sobre a.
func mcpClient(t testing.TB, a *App) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	srv := newMcpServer(a)
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// mcpCallTool invoca una herramienta y devuelve el texto y si fue un error.
func mcpCallTool(t testing.TB, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: error de protocolo: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func setMcp(t testing.TB, a *App, s McpSettings) {
	t.Helper()
	if err := a.SetMcpSettings(s); err != nil {
		t.Fatal(err)
	}
}

func TestMcpToolsListed(t *testing.T) {
	a, _ := newTestApp(t)
	cs := mcpClient(t, a)
	names := map[string]bool{}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names[tool.Name] = true
	}
	info, _ := a.GetMcpInfo()
	if len(names) != len(info.Tools) || len(names) < 50 {
		t.Fatalf("herramientas listadas=%d info=%d", len(names), len(info.Tools))
	}
	for _, n := range []string{"list_servers", "run_sql", "set_object_grant", "create_sftp_user", "get_login_password"} {
		if !names[n] {
			t.Errorf("falta la herramienta %s", n)
		}
	}
	if names["set_setting"] {
		t.Error("no debe exponerse set_setting")
	}
	rr, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: mcpGuideURI})
	if err != nil || len(rr.Contents) != 1 || !strings.Contains(rr.Contents[0].Text, "custom") {
		t.Fatalf("recurso guía: %v %+v", err, rr)
	}
}

func TestMcpDisabledAndModes(t *testing.T) {
	a, _ := newTestApp(t)
	cs := mcpClient(t, a)

	// por defecto desactivado
	txt, isErr := mcpCallTool(t, cs, "list_servers", map[string]any{})
	if !isErr || !strings.Contains(txt, "desactivado") {
		t.Fatalf("desactivado: %q %v", txt, isErr)
	}

	// activado, solo lectura
	setMcp(t, a, McpSettings{Enabled: true, Mode: "readonly"})
	txt, isErr = mcpCallTool(t, cs, "list_servers", map[string]any{})
	if isErr {
		t.Fatalf("list_servers: %q", txt)
	}
	txt, isErr = mcpCallTool(t, cs, "create_database", map[string]any{"server_id": 1, "name": "x"})
	if !isErr || !strings.Contains(txt, "solo lectura") {
		t.Fatalf("readonly debe bloquear mutantes: %q %v", txt, isErr)
	}

	// modo completo: confirm_name incorrecto se rechaza antes de tocar nada
	setMcp(t, a, McpSettings{Enabled: true, Mode: "full"})
	txt, isErr = mcpCallTool(t, cs, "drop_database", map[string]any{"server_id": 1, "name": "x", "confirm_name": "y"})
	if !isErr || !strings.Contains(txt, "confirmación") {
		t.Fatalf("confirm_name: %q %v", txt, isErr)
	}
	txt, isErr = mcpCallTool(t, cs, "delete_sftp_user", map[string]any{"service_id": 1, "name": "u", "confirm_name": ""})
	if !isErr || !strings.Contains(txt, "confirmación") {
		t.Fatalf("confirm_name sftp: %q %v", txt, isErr)
	}
	txt, isErr = mcpCallTool(t, cs, "create_backup", map[string]any{"server_id": 1, "database": "x", "dest_dir": "relativa"})
	if !isErr || !strings.Contains(txt, "absoluta") {
		t.Fatalf("dest_dir: %q %v", txt, isErr)
	}
}

func TestMcpPasswordsAndSql(t *testing.T) {
	a, _ := newTestApp(t)
	cs := mcpClient(t, a)
	setMcp(t, a, McpSettings{Enabled: true, Mode: "readonly"})

	txt, isErr := mcpCallTool(t, cs, "get_login_password", map[string]any{"server_id": 1, "login": "root"})
	if !isErr || !strings.Contains(txt, "contraseñas") {
		t.Fatalf("get_login_password debe bloquearse: %q %v", txt, isErr)
	}
	txt, isErr = mcpCallTool(t, cs, "run_sql", map[string]any{"server_id": 1, "database": "x", "login": "u", "sql": "select 1"})
	if !isErr || !strings.Contains(txt, "run_sql está desactivado") {
		t.Fatalf("run_sql debe bloquearse: %q %v", txt, isErr)
	}

	// con SQL permitido pero sin login, en solo lectura se exige modo completo
	setMcp(t, a, McpSettings{Enabled: true, Mode: "readonly", AllowSql: true})
	a.store.SetSetting("lastDirectory", "") // asegura store abierto
	txt, isErr = mcpCallTool(t, cs, "run_sql", map[string]any{"server_id": 1, "database": "x", "sql": "select 1"})
	if !isErr {
		t.Fatalf("run_sql sin login en readonly debe fallar: %q", txt)
	}
	// varias sentencias
	txt, isErr = mcpCallTool(t, cs, "run_sql", map[string]any{"server_id": 1, "database": "x", "login": "u", "sql": "select 1; drop table t"})
	if !isErr || !strings.Contains(txt, "UNA sentencia") {
		t.Fatalf("multi-sentencia: %q %v", txt, isErr)
	}
}

func TestMcpSetSettingBlocked(t *testing.T) {
	a, _ := newTestApp(t)
	for _, k := range []string{"mcpEnabled", "mcpMode", "MCPAllowSql", "mcpExposePasswords"} {
		if err := a.SetSetting(k, "true"); err == nil {
			t.Errorf("SetSetting(%s) debería rechazarse", k)
		}
	}
	if err := a.SetSetting("lastDirectory", "x"); err != nil {
		t.Error(err)
	}
	if err := a.SetMcpSettings(McpSettings{Mode: "otro"}); err == nil {
		t.Error("modo inválido debería rechazarse")
	}
}

func TestMcpGetInfo(t *testing.T) {
	a, _ := newTestApp(t)
	info, err := a.GetMcpInfo()
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	if !strings.Contains(info.ClaudeCodeCommand, "claude mcp add --scope user andres-tools") ||
		!strings.Contains(info.ClaudeCodeCommand, filepath.Base(exe)) {
		t.Errorf("comando: %s", info.ClaudeCodeCommand)
	}
	var parsed map[string]map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal([]byte(info.ClaudeDesktopJson), &parsed); err != nil {
		t.Fatal(err)
	}
	cmd := parsed["mcpServers"]["andres-tools"]
	if cmd.Command != info.ExePath || len(cmd.Args) != 1 || cmd.Args[0] != "mcp" {
		t.Errorf("json: %+v", cmd)
	}
	var mut int
	for _, tl := range info.Tools {
		if tl.Mutating {
			mut++
		}
	}
	if mut == 0 || mut == len(info.Tools) || info.Settings.Mode != "readonly" || info.Settings.Enabled {
		t.Errorf("info: mutating=%d total=%d settings=%+v", mut, len(info.Tools), info.Settings)
	}
}

// TestMcpRealExe compila el ejecutable y lo consulta por stdio como lo haría un agente.
func TestMcpRealExe(t *testing.T) {
	if testing.Short() {
		t.Skip("compila el ejecutable")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "at-mcp-test.exe")
	build := exec.Command("go", "build", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	data, err := os.MkdirTemp("", "dbm-mcp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBMANAGER_DATA_DIR", data)
	t.Cleanup(func() {
		for i := 0; i < 10; i++ {
			if os.RemoveAll(data) == nil {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	})
	r := mcpProbe(exe)
	if !r.Ok || r.ToolCount < 50 || r.ServerVersion == "" {
		t.Fatalf("probe: %+v", r)
	}
}
