package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpServerName    = "andres-tools"
	mcpServerVersion = "1.0.0"

	settingMcpEnabled         = "mcpEnabled"
	settingMcpMode            = "mcpMode"
	settingMcpAllowSql        = "mcpAllowSql"
	settingMcpExposePasswords = "mcpExposePasswords"

	mcpModeReadonly = "readonly"
	mcpModeFull     = "full"

	mcpGuideURI = "andres-tools://guide"
)

const mcpGuide = `# Andres Tools: guía del modelo de permisos

## Servidores
Cada servidor (id numérico) es un SQL Server o PostgreSQL, local (contenedor Docker administrado) o remoto.
Se administran con el usuario **root** del servidor (get_root_status / configure_root).

## Niveles de acceso a una base (login x base de datos)
- owner: dueño de la base (control total).
- readwrite: lectura y escritura de todas las tablas.
- readonly: solo lectura de todas las tablas.
- custom: puede entrar a la base pero sin roles globales; solo tiene los permisos por esquema/tabla que se le otorguen.
- none: sin acceso.

## Permisos por esquema y tabla (nivel custom)
Privilegios: SELECT, INSERT, UPDATE, DELETE, EXECUTE, ALTER.
- set_object_grant con table vacío = permiso sobre todo el esquema.
- set_object_grant con table = permiso solo sobre esa tabla.
- Los privilegios indicados reemplazan a los anteriores de ese objeto; lista vacía = quitar.

## Flujo típico para un usuario con acceso limitado
1. create_database / create_schema.
2. create_database_user con level=custom (crea el login y el usuario en la base).
3. set_object_grant con los privilegios necesarios.
4. Verificar con run_sql usando login=<ese usuario> (requiere remember_login_password si el login se creó fuera de la app).
   Un "permission denied" es un resultado normal (ok=false), no un fallo de la herramienta.

## Seguridad
- El servidor MCP puede estar desactivado, en modo solo lectura o completo, y las contraseñas/SQL se habilitan por separado desde Andres Tools > MCP.
- Las acciones destructivas piden confirm_name igual al nombre del objeto.
`

// mcpSettings lee los ajustes en cada llamada (el usuario puede cambiarlos desde la interfaz).
func (a *App) mcpSettings() McpSettings {
	s := McpSettings{Mode: mcpModeReadonly}
	st, err := a.db()
	if err != nil {
		return s
	}
	get := func(k string) string { v, _ := st.GetSetting(k); return strings.TrimSpace(v) }
	s.Enabled = get(settingMcpEnabled) == "true"
	if get(settingMcpMode) == mcpModeFull {
		s.Mode = mcpModeFull
	}
	s.AllowSql = get(settingMcpAllowSql) == "true"
	s.ExposePasswords = get(settingMcpExposePasswords) == "true"
	return s
}

// SetMcpSettings guarda los ajustes del servidor MCP (solo desde la interfaz; el agente no puede cambiarlos).
func (a *App) SetMcpSettings(s McpSettings) error {
	st, err := a.db()
	if err != nil {
		return err
	}
	if s.Mode != mcpModeReadonly && s.Mode != mcpModeFull {
		return errors.New("modo MCP inválido: use readonly o full")
	}
	b := func(v bool) string {
		if v {
			return "true"
		}
		return "false"
	}
	for k, v := range map[string]string{settingMcpEnabled: b(s.Enabled), settingMcpMode: s.Mode,
		settingMcpAllowSql: b(s.AllowSql), settingMcpExposePasswords: b(s.ExposePasswords)} {
		if err := st.SetSetting(k, v); err != nil {
			a.logOp(0, "mcp-settings", "", err)
			return err
		}
	}
	a.logOp(0, "mcp-settings", fmt.Sprintf("enabled=%s mode=%s sql=%s passwords=%s", b(s.Enabled), s.Mode, b(s.AllowSql), b(s.ExposePasswords)), nil)
	return nil
}

func mcpDevBuild(exe string) bool {
	l := strings.ToLower(exe)
	if strings.Contains(l, `\build\bin\`) || strings.Contains(l, "/build/bin/") {
		return false
	}
	tmp := strings.ToLower(os.TempDir())
	return strings.Contains(l, "-dev") || (tmp != "" && strings.HasPrefix(l, tmp))
}

// GetMcpInfo devuelve los ajustes, los comandos de conexión y la lista de herramientas.
func (a *App) GetMcpInfo() (McpInfo, error) {
	exe, err := os.Executable()
	if err != nil {
		return McpInfo{}, err
	}
	if r, e := filepath.EvalSymlinks(exe); e == nil {
		exe = r
	}
	desktop := map[string]any{"mcpServers": map[string]any{mcpServerName: map[string]any{"command": exe, "args": []string{"mcp"}}}}
	dj, _ := json.MarshalIndent(desktop, "", "  ")
	info := McpInfo{
		Settings:          a.mcpSettings(),
		ExePath:           exe,
		ClaudeCodeCommand: fmt.Sprintf(`claude mcp add --scope user %s -- "%s" mcp`, mcpServerName, exe),
		ClaudeDesktopJson: string(dj),
		GenericJson:       string(dj),
		DevBuild:          mcpDevBuild(exe),
		Tools:             []McpToolInfo{},
	}
	for _, d := range mcpToolDefs() {
		info.Tools = append(info.Tools, McpToolInfo{Name: d.Name, Description: d.Description, Mutating: d.Mutating})
	}
	return info, nil
}

// TestMcpServer levanta el propio ejecutable en modo MCP y lo consulta como lo haría un agente.
func (a *App) TestMcpServer() (McpTestResult, error) {
	exe, err := os.Executable()
	if err != nil {
		return McpTestResult{}, err
	}
	return mcpProbe(exe), nil
}

// mcpProbe ejecuta "<exe> mcp", inicializa la sesión por stdio y lista las herramientas.
func mcpProbe(exe string) McpTestResult {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.Command(exe, "mcp")
	hideWindow(cmd)
	client := mcp.NewClient(&mcp.Implementation{Name: "andres-tools-selftest", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return McpTestResult{Message: "No se pudo iniciar el servidor MCP: " + err.Error()}
	}
	defer cs.Close()
	res := McpTestResult{}
	if ir := cs.InitializeResult(); ir != nil && ir.ServerInfo != nil {
		res.ServerVersion = ir.ServerInfo.Version
	}
	for _, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return McpTestResult{Message: "Error al listar herramientas: " + err.Error(), ServerVersion: res.ServerVersion}
		}
		res.ToolCount++
	}
	res.Ok = true
	res.Message = fmt.Sprintf("El servidor MCP respondió correctamente (%d herramientas).", res.ToolCount)
	return res
}

// newMcpServer construye el servidor MCP con todas las herramientas del registro.
func newMcpServer(a *App) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: mcpServerName, Version: mcpServerVersion}, &mcp.ServerOptions{
		Instructions: "Andres Tools: administra servidores SQL Server/PostgreSQL y servicios SFTP. Lea el recurso " + mcpGuideURI +
			" para entender el modelo de permisos. Si una herramienta indica que MCP está desactivado o en solo lectura, pida al usuario cambiarlo en Andres Tools > MCP.",
	})
	for _, d := range mcpToolDefs() {
		d.register(s, a)
	}
	s.AddResource(&mcp.Resource{URI: mcpGuideURI, Name: "guide", Title: "Guía del modelo de permisos",
		Description: "Niveles de acceso, privilegios por esquema/tabla y flujo recomendado.", MIMEType: "text/markdown"},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: mcpGuideURI, MIMEType: "text/markdown", Text: mcpGuide}}}, nil
		})
	return s
}

// runMcpStdio arranca el servidor MCP por stdio (modo "mcp" del ejecutable). Nada más puede escribir en stdout.
func runMcpStdio() int {
	// stdout es el canal del protocolo: se reserva y cualquier escritura accidental (fmt.Println, log) va a stderr.
	proto := os.Stdout
	os.Stdout = os.Stderr
	log.SetOutput(os.Stderr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := NewApp()
	a.emit = func(string, any) {} // el emisor por defecto usa el runtime de Wails, que no existe aquí
	a.startup(ctx)
	defer a.shutdown(ctx)
	if a.initErr != nil {
		fmt.Fprintln(os.Stderr, "andres-tools mcp: no se pudo abrir el almacenamiento:", a.initErr)
	}
	s := newMcpServer(a)
	t := &mcp.IOTransport{Reader: os.Stdin, Writer: nopWriteCloser{proto}}
	if err := s.Run(ctx, t); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "andres-tools mcp:", err)
		return 1
	}
	return 0
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
