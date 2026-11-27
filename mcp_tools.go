package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"dbmanager/internal/validate"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	errMcpDisabled = "El servidor MCP está desactivado. Actívelo en Andres Tools > MCP."
	errMcpReadonly = "El servidor MCP está en modo solo lectura. Para permitir cambios, el usuario debe elegir el modo \"Completo\" en Andres Tools > MCP."
	maskedPassword = "********"
)

// mcpCall acompaña a cada llamada: ajustes vigentes y datos para el registro de operaciones.
type mcpCall struct {
	a        *App
	set      McpSettings
	serverID int64
	target   string
}

// at fija el servidor y el destino que se registrarán (nunca contraseñas).
func (c *mcpCall) at(serverID int64, target string) {
	c.serverID, c.target = serverID, target
}

// mcpToolDef es la única fuente de verdad de las herramientas: se usa para registrarlas y para listarlas en la GUI.
type mcpToolDef struct {
	Name        string
	Description string
	Mutating    bool
	register    func(s *mcp.Server, a *App)
}

// tool define una herramienta con entrada tipada. El resultado se serializa como JSON en el contenido de texto.
func tool[In any](name, desc string, mutating bool, h func(ctx context.Context, c *mcpCall, in In) (any, error)) mcpToolDef {
	return mcpToolDef{Name: name, Description: desc, Mutating: mutating, register: func(s *mcp.Server, a *App) {
		mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc},
			func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
				c := &mcpCall{a: a, set: a.mcpSettings()}
				var out any
				var err error
				switch {
				case !c.set.Enabled:
					err = errors.New(errMcpDisabled)
				case mutating && c.set.Mode != mcpModeFull:
					err = errors.New(errMcpReadonly)
				default:
					out, err = h(ctx, c, in)
				}
				a.logOp(c.serverID, "mcp:"+name, c.target, err)
				if err != nil {
					return nil, nil, err
				}
				if out == nil {
					out = map[string]any{"ok": true}
				}
				b, merr := json.Marshal(out)
				if merr != nil {
					return nil, nil, merr
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
			})
	}}
}

func confirmName(name, confirm string) error {
	if strings.TrimSpace(name) == "" || name != confirm {
		return fmt.Errorf("confirmación inválida: confirm_name debe ser exactamente %q", name)
	}
	return nil
}

func okResult(msg string) any { return map[string]any{"ok": true, "message": msg} }

// ---------- entradas ----------

type noArgs struct{}

type serverArg struct {
	ServerID int64 `json:"server_id" jsonschema:"ID del servidor (ver list_servers)"`
}

type serverLogsArgs struct {
	ServerID int64 `json:"server_id" jsonschema:"ID del servidor"`
	Tail     int   `json:"tail,omitempty" jsonschema:"líneas finales del log (por defecto 100, máximo 2000)"`
}

type serverDbArgs struct {
	ServerID int64  `json:"server_id" jsonschema:"ID del servidor"`
	Database string `json:"database" jsonschema:"nombre de la base de datos"`
}

type connStringArgs struct {
	ServerID int64  `json:"server_id" jsonschema:"ID del servidor"`
	Database string `json:"database,omitempty" jsonschema:"base de datos (opcional)"`
	Login    string `json:"login,omitempty" jsonschema:"usuario (por defecto el root del servidor)"`
	Format   string `json:"format" jsonschema:"formato: dotnet | jdbc | sqlcmd (solo mssql) | psql (solo postgres)"`
}

type loginArgs struct {
	ServerID int64  `json:"server_id" jsonschema:"ID del servidor"`
	Login    string `json:"login" jsonschema:"nombre del login/usuario"`
}

type limitArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"cantidad máxima de operaciones (por defecto 50, máximo 500)"`
}

type idArg struct {
	ID int64 `json:"id" jsonschema:"ID del servicio SFTP"`
}

type serviceArg struct {
	ServiceID int64 `json:"service_id" jsonschema:"ID del servicio SFTP"`
}

type createDbArgs struct {
	ServerID int64  `json:"server_id" jsonschema:"ID del servidor"`
	Name     string `json:"name" jsonschema:"nombre de la base de datos"`
	Owner    string `json:"owner,omitempty" jsonschema:"login dueño (opcional)"`
}

type confirmArgs struct {
	ServerID    int64  `json:"server_id" jsonschema:"ID del servidor"`
	Name        string `json:"name" jsonschema:"nombre del objeto a eliminar"`
	ConfirmName string `json:"confirm_name" jsonschema:"debe ser igual a name para confirmar"`
}

type createLoginArgs struct {
	ServerID    int64    `json:"server_id" jsonschema:"ID del servidor"`
	Name        string   `json:"name" jsonschema:"nombre del login"`
	Password    string   `json:"password" jsonschema:"contraseña del login"`
	ServerRoles []string `json:"server_roles,omitempty" jsonschema:"roles de servidor (p. ej. dbcreator en mssql)"`
}

type changePwArgs struct {
	ServerID int64  `json:"server_id" jsonschema:"ID del servidor"`
	Name     string `json:"name" jsonschema:"nombre del login"`
	Password string `json:"password" jsonschema:"nueva contraseña"`
}

type serverRolesArgs struct {
	ServerID    int64    `json:"server_id" jsonschema:"ID del servidor"`
	Name        string   `json:"name" jsonschema:"nombre del login"`
	ServerRoles []string `json:"server_roles" jsonschema:"lista completa de roles de servidor (reemplaza la actual)"`
}

type createDbUserArgs struct {
	ServerID int64  `json:"server_id" jsonschema:"ID del servidor"`
	Database string `json:"database" jsonschema:"base de datos a la que tendrá acceso"`
	Name     string `json:"name" jsonschema:"nombre del usuario/login"`
	Password string `json:"password" jsonschema:"contraseña"`
	Level    string `json:"level" jsonschema:"owner | readwrite | readonly | custom"`
}

type dbAccessArgs struct {
	ServerID int64  `json:"server_id" jsonschema:"ID del servidor"`
	Login    string `json:"login" jsonschema:"login existente"`
	Database string `json:"database" jsonschema:"base de datos"`
	Level    string `json:"level" jsonschema:"owner | readwrite | readonly | custom | none"`
}

type createSchemaArgs struct {
	ServerID int64  `json:"server_id" jsonschema:"ID del servidor"`
	Database string `json:"database" jsonschema:"base de datos"`
	Name     string `json:"name" jsonschema:"nombre del esquema"`
}

type objectGrantArgs struct {
	ServerID   int64    `json:"server_id" jsonschema:"ID del servidor"`
	Database   string   `json:"database" jsonschema:"base de datos"`
	Login      string   `json:"login" jsonschema:"login que recibe los permisos"`
	Schema     string   `json:"schema" jsonschema:"esquema"`
	Table      string   `json:"table,omitempty" jsonschema:"tabla; vacío = todo el esquema"`
	Privileges []string `json:"privileges" jsonschema:"SELECT, INSERT, UPDATE, DELETE, EXECUTE, ALTER; lista vacía = quitar permisos"`
}

type configureRootArgs struct {
	ServerID          int64  `json:"server_id" jsonschema:"ID del servidor"`
	User              string `json:"user" jsonschema:"nombre del usuario root a crear/usar"`
	Password          string `json:"password" jsonschema:"contraseña del usuario root"`
	BootstrapUser     string `json:"bootstrap_user,omitempty" jsonschema:"admin existente (p. ej. sa) si el usuario actual no tiene permisos"`
	BootstrapPassword string `json:"bootstrap_password,omitempty" jsonschema:"contraseña del admin existente"`
	DropPrevious      bool   `json:"drop_previous,omitempty" jsonschema:"eliminar el usuario root anterior"`
}

type createServiceArgs struct {
	Name          string `json:"name" jsonschema:"nombre del servicio"`
	Engine        string `json:"engine" jsonschema:"mssql | postgres"`
	Image         string `json:"image,omitempty" jsonschema:"imagen Docker (opcional)"`
	Port          int    `json:"port,omitempty" jsonschema:"puerto en el host (opcional)"`
	AdminPassword string `json:"admin_password" jsonschema:"contraseña del administrador del motor (sa/postgres)"`
	DataMode      string `json:"data_mode,omitempty" jsonschema:"volume | bind"`
	DataPath      string `json:"data_path,omitempty" jsonschema:"nombre del volumen o carpeta del host"`
	MemoryMB      int    `json:"memory_mb,omitempty" jsonschema:"memoria en MB (mssql)"`
	Edition       string `json:"edition,omitempty" jsonschema:"edición de SQL Server (Developer, Express...)"`
	RootUser      string `json:"root_user,omitempty" jsonschema:"usuario root (vacío = predeterminado de Ajustes)"`
	RootPassword  string `json:"root_password,omitempty" jsonschema:"contraseña root (vacío = predeterminada de Ajustes)"`
}

type createBackupArgs struct {
	ServerID int64  `json:"server_id" jsonschema:"ID del servidor"`
	Database string `json:"database" jsonschema:"base de datos a respaldar"`
	DestDir  string `json:"dest_dir" jsonschema:"carpeta de destino, ruta absoluta"`
}

type rememberPwArgs struct {
	ServerID int64  `json:"server_id" jsonschema:"ID del servidor"`
	Login    string `json:"login" jsonschema:"login existente"`
	Password string `json:"password" jsonschema:"su contraseña (se verifica conectando)"`
}

type createSftpUserArgs struct {
	ServiceID int64  `json:"service_id" jsonschema:"ID del servicio SFTP"`
	Name      string `json:"name" jsonschema:"nombre del usuario"`
	Password  string `json:"password" jsonschema:"contraseña"`
	UploadDir string `json:"upload_dir,omitempty" jsonschema:"carpeta de subida (opcional)"`
}

type deleteSftpArgs struct {
	ServiceID   int64  `json:"service_id" jsonschema:"ID del servicio SFTP"`
	Name        string `json:"name" jsonschema:"nombre del usuario o carpeta"`
	ConfirmName string `json:"confirm_name" jsonschema:"debe ser igual a name para confirmar"`
	DeleteFiles bool   `json:"delete_files,omitempty" jsonschema:"borrar también los archivos"`
}

type sftpPwArgs struct {
	ServiceID int64  `json:"service_id" jsonschema:"ID del servicio SFTP"`
	Name      string `json:"name" jsonschema:"nombre del usuario"`
	Password  string `json:"password" jsonschema:"nueva contraseña"`
}

type sftpFolderArgs struct {
	ServiceID int64  `json:"service_id" jsonschema:"ID del servicio SFTP"`
	Name      string `json:"name" jsonschema:"nombre de la carpeta"`
}

type sftpFolderAccessArgs struct {
	ServiceID int64  `json:"service_id" jsonschema:"ID del servicio SFTP"`
	Folder    string `json:"folder" jsonschema:"nombre de la carpeta"`
	User      string `json:"user" jsonschema:"usuario SFTP"`
	Level     string `json:"level" jsonschema:"readwrite | readonly | none"`
}

type testSftpArgs struct {
	ServiceID int64  `json:"service_id" jsonschema:"ID del servicio SFTP"`
	User      string `json:"user" jsonschema:"usuario SFTP"`
	Password  string `json:"password" jsonschema:"contraseña"`
}

type runSqlArgs struct {
	ServerID int64  `json:"server_id" jsonschema:"ID del servidor"`
	Database string `json:"database" jsonschema:"base de datos a la que conectar"`
	Login    string `json:"login,omitempty" jsonschema:"conectar COMO este login (usa su contraseña guardada). Si se omite se usa el root y se requiere modo completo"`
	SQL      string `json:"sql" jsonschema:"una sola sentencia SQL"`
	MaxRows  int    `json:"max_rows,omitempty" jsonschema:"máximo de filas a devolver (por defecto 100, máximo 1000)"`
}

// ---------- registro ----------

func mcpToolDefs() []mcpToolDef {
	ro, rw := false, true
	app := func(c *mcpCall) *App { return c.a }
	return []mcpToolDef{
		// ----- Solo lectura -----
		tool("list_servers", "Lista los servidores de base de datos registrados (id, nombre, motor, host, puerto, modo).", ro,
			func(ctx context.Context, c *mcpCall, _ noArgs) (any, error) { return app(c).ListServers() }),
		tool("get_server", "Devuelve los datos de un servidor (sin contraseñas).", ro,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return c.a.GetServer(in.ServerID)
			}),
		tool("server_status", "Estado del servidor: contenedor (running/stopped) y si responde.", ro,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return c.a.ServerStatus(in.ServerID)
			}),
		tool("test_connection", "Prueba la conexión con el servidor usando el usuario root guardado y devuelve la versión del motor.", ro,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return c.a.TestConnection(in.ServerID)
			}),
		tool("server_logs", "Últimas líneas del log del contenedor del servidor (solo servidores locales).", ro,
			func(ctx context.Context, c *mcpCall, in serverLogsArgs) (any, error) {
				c.at(in.ServerID, "")
				t := in.Tail
				if t <= 0 {
					t = 100
				}
				if t > 2000 {
					t = 2000
				}
				s, err := c.a.ServerLogs(in.ServerID, t)
				return map[string]any{"logs": s}, err
			}),
		tool("list_databases", "Lista las bases de datos del servidor (nombre, tamaño, dueño, estado).", ro,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return c.a.ListDatabases(in.ServerID)
			}),
		tool("list_logins", "Lista los logins/roles del servidor con sus roles de servidor.", ro,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return c.a.ListLogins(in.ServerID)
			}),
		tool("get_permission_matrix", "Matriz login x base de datos con el nivel de acceso (owner/readwrite/readonly/custom/none).", ro,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return c.a.GetPermissionMatrix(in.ServerID)
			}),
		tool("list_schemas", "Lista los esquemas de una base de datos con sus tablas.", ro,
			func(ctx context.Context, c *mcpCall, in serverDbArgs) (any, error) {
				c.at(in.ServerID, in.Database)
				return c.a.ListSchemas(in.ServerID, in.Database)
			}),
		tool("list_object_grants", "Lista los permisos por esquema/tabla otorgados en una base de datos.", ro,
			func(ctx context.Context, c *mcpCall, in serverDbArgs) (any, error) {
				c.at(in.ServerID, in.Database)
				return c.a.ListObjectGrants(in.ServerID, in.Database)
			}),
		tool("list_connection_users", "Usuarios con los que se puede conectar a una base (root primero) y si hay contraseña guardada.", ro,
			func(ctx context.Context, c *mcpCall, in serverDbArgs) (any, error) {
				c.at(in.ServerID, in.Database)
				return c.a.ListConnectionUsers(in.ServerID, in.Database)
			}),
		tool("get_connection_string", "Arma una cadena de conexión (dotnet, jdbc, sqlcmd o psql). La contraseña solo se incluye si el usuario habilitó 'exponer contraseñas'; si no, aparece como ********.", ro,
			toolConnString),
		tool("get_login_password", "Devuelve la contraseña guardada de un login. Solo funciona si el usuario habilitó 'exponer contraseñas' en Andres Tools > MCP.", ro,
			func(ctx context.Context, c *mcpCall, in loginArgs) (any, error) {
				c.at(in.ServerID, in.Login)
				if !c.set.ExposePasswords {
					return nil, errors.New("exponer contraseñas está desactivado. El usuario debe habilitarlo en Andres Tools > MCP")
				}
				pw, err := c.a.GetLoginPassword(in.ServerID, in.Login)
				if err != nil {
					return nil, err
				}
				return map[string]any{"login": in.Login, "known": pw != "", "password": pw}, nil
			}),
		tool("get_root_status", "Indica con qué usuario root se administra el servidor y si es administrador total.", ro,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return c.a.GetRootStatus(in.ServerID)
			}),
		tool("get_root_defaults", "Usuario root predeterminado para servicios nuevos (sin contraseña).", ro,
			func(ctx context.Context, c *mcpCall, _ noArgs) (any, error) { return c.a.GetRootDefaults() }),
		tool("list_backups", "Historial de backups del servidor.", ro,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return c.a.ListBackups(in.ServerID)
			}),
		tool("list_restores", "Historial de restauraciones del servidor.", ro,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return c.a.ListRestores(in.ServerID)
			}),
		tool("list_operations", "Últimas operaciones registradas (incluye las hechas por MCP, con prefijo mcp:).", ro,
			func(ctx context.Context, c *mcpCall, in limitArgs) (any, error) {
				l := in.Limit
				if l <= 0 {
					l = 50
				}
				if l > 500 {
					l = 500
				}
				return c.a.ListOperations(l)
			}),
		tool("list_sftp_services", "Lista los servicios SFTP registrados.", ro,
			func(ctx context.Context, c *mcpCall, _ noArgs) (any, error) { return c.a.ListSftpServices() }),
		tool("sftp_service_status", "Estado de un servicio SFTP.", ro,
			func(ctx context.Context, c *mcpCall, in idArg) (any, error) {
				c.at(in.ID, "")
				return c.a.SftpServiceStatus(in.ID)
			}),
		tool("list_sftp_users", "Lista los usuarios de un servicio SFTP (sin contraseñas).", ro,
			func(ctx context.Context, c *mcpCall, in serviceArg) (any, error) {
				c.at(in.ServiceID, "")
				return c.a.ListSftpUsers(in.ServiceID)
			}),
		tool("list_sftp_folders", "Lista las carpetas compartidas de un servicio SFTP y el acceso de cada usuario.", ro,
			func(ctx context.Context, c *mcpCall, in serviceArg) (any, error) {
				c.at(in.ServiceID, "")
				return c.a.ListSftpFolders(in.ServiceID)
			}),
		tool("sftp_host_fingerprints", "Huellas de las llaves de host de un servicio SFTP administrado.", ro,
			func(ctx context.Context, c *mcpCall, in idArg) (any, error) {
				c.at(in.ID, "")
				return c.a.GetSftpHostFingerprints(in.ID)
			}),
		tool("check_environment", "Verifica Docker y el entorno (versión, SO, advertencias).", ro,
			func(ctx context.Context, c *mcpCall, _ noArgs) (any, error) { return c.a.CheckEnvironment() }),

		// ----- Prueba con SQL -----
		tool("run_sql", "Ejecuta UNA sentencia SQL conectando COMO el login indicado (con su contraseña guardada) para verificar permisos. Requiere 'permitir SQL' en Andres Tools > MCP. Un error de permisos devuelve ok=false (es un resultado normal). Sin login usa el root y exige modo completo.", ro,
			toolRunSQL),

		// ----- Mutantes (modo completo) -----
		tool("create_database", "Crea una base de datos.", rw,
			func(ctx context.Context, c *mcpCall, in createDbArgs) (any, error) {
				c.at(in.ServerID, in.Name)
				return nil, c.a.CreateDatabase(in.ServerID, in.Name, in.Owner)
			}),
		tool("drop_database", "Elimina una base de datos. Destructivo: confirm_name debe ser igual a name.", rw,
			func(ctx context.Context, c *mcpCall, in confirmArgs) (any, error) {
				c.at(in.ServerID, in.Name)
				if err := confirmName(in.Name, in.ConfirmName); err != nil {
					return nil, err
				}
				return nil, c.a.DropDatabase(in.ServerID, in.Name)
			}),
		tool("create_login", "Crea un login con contraseña y roles de servidor.", rw,
			func(ctx context.Context, c *mcpCall, in createLoginArgs) (any, error) {
				c.at(in.ServerID, in.Name)
				return nil, c.a.CreateLogin(in.ServerID, CreateLoginRequest{Name: in.Name, Password: in.Password, ServerRoles: in.ServerRoles})
			}),
		tool("drop_login", "Elimina un login. Destructivo: confirm_name debe ser igual a name.", rw,
			func(ctx context.Context, c *mcpCall, in confirmArgs) (any, error) {
				c.at(in.ServerID, in.Name)
				if err := confirmName(in.Name, in.ConfirmName); err != nil {
					return nil, err
				}
				return nil, c.a.DropLogin(in.ServerID, in.Name)
			}),
		tool("change_password", "Cambia la contraseña de un login.", rw,
			func(ctx context.Context, c *mcpCall, in changePwArgs) (any, error) {
				c.at(in.ServerID, in.Name)
				return nil, c.a.ChangePassword(in.ServerID, in.Name, in.Password)
			}),
		tool("set_server_roles", "Reemplaza los roles de servidor de un login.", rw,
			func(ctx context.Context, c *mcpCall, in serverRolesArgs) (any, error) {
				c.at(in.ServerID, in.Name+": "+strings.Join(in.ServerRoles, ","))
				return nil, c.a.SetServerRoles(in.ServerID, in.Name, in.ServerRoles)
			}),
		tool("create_database_user", "Crea un login y le da acceso a una base con el nivel indicado (owner/readwrite/readonly/custom). Con custom use luego set_object_grant.", rw,
			func(ctx context.Context, c *mcpCall, in createDbUserArgs) (any, error) {
				c.at(in.ServerID, in.Database+"/"+in.Name+":"+in.Level)
				return nil, c.a.CreateDatabaseUser(in.ServerID, in.Database, CreateLoginRequest{Name: in.Name, Password: in.Password}, in.Level)
			}),
		tool("set_database_access", "Cambia el nivel de acceso de un login a una base (owner/readwrite/readonly/custom/none).", rw,
			func(ctx context.Context, c *mcpCall, in dbAccessArgs) (any, error) {
				c.at(in.ServerID, in.Database+"/"+in.Login+":"+in.Level)
				return nil, c.a.SetDatabaseAccess(in.ServerID, in.Login, in.Database, in.Level)
			}),
		tool("create_schema", "Crea un esquema en una base de datos.", rw,
			func(ctx context.Context, c *mcpCall, in createSchemaArgs) (any, error) {
				c.at(in.ServerID, in.Database+"/"+in.Name)
				return nil, c.a.CreateSchema(in.ServerID, in.Database, in.Name)
			}),
		tool("set_object_grant", "Define los privilegios de un login sobre un esquema (table vacío) o una tabla. Reemplaza los anteriores de ese objeto.", rw,
			func(ctx context.Context, c *mcpCall, in objectGrantArgs) (any, error) {
				c.at(in.ServerID, fmt.Sprintf("%s/%s: %s.%s=%s", in.Database, in.Login, in.Schema, in.Table, strings.Join(in.Privileges, ",")))
				return nil, c.a.SetObjectGrant(in.ServerID, in.Database, ObjectGrantRequest{Login: in.Login, Schema: in.Schema, Table: in.Table, Privileges: in.Privileges})
			}),
		tool("configure_root", "Crea o cambia el usuario root del servidor (en el motor y en Andres Tools).", rw,
			func(ctx context.Context, c *mcpCall, in configureRootArgs) (any, error) {
				c.at(in.ServerID, in.User)
				return c.a.ConfigureRoot(in.ServerID, ConfigureRootRequest{User: in.User, Password: in.Password,
					BootstrapUser: in.BootstrapUser, BootstrapPassword: in.BootstrapPassword, DropPrevious: in.DropPrevious})
			}),
		tool("start_server", "Inicia el contenedor de un servidor local.", rw,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return nil, c.a.StartServer(in.ServerID)
			}),
		tool("stop_server", "Detiene el contenedor de un servidor local.", rw,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return nil, c.a.StopServer(in.ServerID)
			}),
		tool("restart_server", "Reinicia el contenedor de un servidor local.", rw,
			func(ctx context.Context, c *mcpCall, in serverArg) (any, error) {
				c.at(in.ServerID, "")
				return nil, c.a.RestartServer(in.ServerID)
			}),
		tool("create_local_service", "Crea un servicio de base de datos local en Docker (SQL Server o PostgreSQL).", rw,
			func(ctx context.Context, c *mcpCall, in createServiceArgs) (any, error) {
				c.at(0, in.Name)
				return c.a.CreateLocalService(CreateServiceRequest{Name: in.Name, Engine: in.Engine, Image: in.Image, Port: in.Port,
					AdminPassword: in.AdminPassword, DataMode: in.DataMode, DataPath: in.DataPath, MemoryMB: in.MemoryMB,
					Edition: in.Edition, RootUser: in.RootUser, RootPassword: in.RootPassword})
			}),
		tool("create_backup", "Crea un backup de una base de datos en una carpeta del equipo (ruta absoluta).", rw,
			func(ctx context.Context, c *mcpCall, in createBackupArgs) (any, error) {
				c.at(in.ServerID, in.Database)
				if !filepath.IsAbs(in.DestDir) {
					return nil, errors.New("dest_dir debe ser una ruta absoluta")
				}
				return c.a.CreateBackup(BackupRequest{ServerID: in.ServerID, Database: in.Database, DestDir: in.DestDir})
			}),
		tool("remember_login_password", "Guarda en Andres Tools la contraseña de un login existente (tras verificar que conecta), para poder usar run_sql con él.", rw,
			func(ctx context.Context, c *mcpCall, in rememberPwArgs) (any, error) {
				c.at(in.ServerID, in.Login)
				return nil, c.a.RememberLoginPassword(in.ServerID, in.Login, in.Password)
			}),
		tool("start_sftp_service", "Inicia un servicio SFTP.", rw,
			func(ctx context.Context, c *mcpCall, in idArg) (any, error) {
				c.at(in.ID, "")
				return nil, c.a.StartSftpService(in.ID)
			}),
		tool("stop_sftp_service", "Detiene un servicio SFTP.", rw,
			func(ctx context.Context, c *mcpCall, in idArg) (any, error) {
				c.at(in.ID, "")
				return nil, c.a.StopSftpService(in.ID)
			}),
		tool("restart_sftp_service", "Reinicia un servicio SFTP.", rw,
			func(ctx context.Context, c *mcpCall, in idArg) (any, error) {
				c.at(in.ID, "")
				return nil, c.a.RestartSftpService(in.ID)
			}),
		tool("create_sftp_user", "Crea un usuario SFTP con contraseña (la autenticación por llave no está disponible vía MCP).", rw,
			func(ctx context.Context, c *mcpCall, in createSftpUserArgs) (any, error) {
				c.at(in.ServiceID, in.Name)
				r, err := c.a.CreateSftpUser(in.ServiceID, CreateSftpUserRequest{Name: in.Name, Password: in.Password, UploadDir: in.UploadDir})
				if err != nil {
					return nil, err
				}
				return map[string]any{"user": r.User, "connectionHints": r.ConnectionHints}, nil
			}),
		tool("delete_sftp_user", "Elimina un usuario SFTP. Destructivo: confirm_name debe ser igual a name.", rw,
			func(ctx context.Context, c *mcpCall, in deleteSftpArgs) (any, error) {
				c.at(in.ServiceID, in.Name)
				if err := confirmName(in.Name, in.ConfirmName); err != nil {
					return nil, err
				}
				return nil, c.a.DeleteSftpUser(in.ServiceID, in.Name, in.DeleteFiles)
			}),
		tool("set_sftp_user_password", "Cambia la contraseña de un usuario SFTP.", rw,
			func(ctx context.Context, c *mcpCall, in sftpPwArgs) (any, error) {
				c.at(in.ServiceID, in.Name)
				return nil, c.a.SetSftpUserPassword(in.ServiceID, in.Name, in.Password)
			}),
		tool("create_sftp_folder", "Crea una carpeta compartida en un servicio SFTP.", rw,
			func(ctx context.Context, c *mcpCall, in sftpFolderArgs) (any, error) {
				c.at(in.ServiceID, in.Name)
				return nil, c.a.CreateSftpFolder(in.ServiceID, in.Name)
			}),
		tool("delete_sftp_folder", "Elimina una carpeta compartida SFTP. Destructivo: confirm_name debe ser igual a name.", rw,
			func(ctx context.Context, c *mcpCall, in deleteSftpArgs) (any, error) {
				c.at(in.ServiceID, in.Name)
				if err := confirmName(in.Name, in.ConfirmName); err != nil {
					return nil, err
				}
				return nil, c.a.DeleteSftpFolder(in.ServiceID, in.Name, in.DeleteFiles)
			}),
		tool("set_sftp_folder_access", "Define el acceso de un usuario SFTP a una carpeta (readwrite/readonly/none).", rw,
			func(ctx context.Context, c *mcpCall, in sftpFolderAccessArgs) (any, error) {
				c.at(in.ServiceID, in.Folder+":"+in.User+":"+in.Level)
				return nil, c.a.SetSftpFolderAccess(in.ServiceID, in.Folder, in.User, in.Level)
			}),
		tool("test_sftp_login", "Prueba el inicio de sesión SFTP con usuario y contraseña y lista el directorio raíz.", rw,
			func(ctx context.Context, c *mcpCall, in testSftpArgs) (any, error) {
				c.at(in.ServiceID, in.User)
				return c.a.TestSftpLogin(SftpLoginTest{ServiceID: in.ServiceID, User: in.User, Password: in.Password})
			}),
	}
}

// toolConnString arma la cadena de conexión según el motor y el formato.
func toolConnString(ctx context.Context, c *mcpCall, in connStringArgs) (any, error) {
	c.at(in.ServerID, in.Database)
	sv, err := c.a.GetServer(in.ServerID)
	if err != nil {
		return nil, err
	}
	login := strings.TrimSpace(in.Login)
	if login == "" {
		login = sv.AdminUser
	}
	pw := maskedPassword
	if c.set.ExposePasswords {
		p, err := c.a.GetLoginPassword(in.ServerID, login)
		if err != nil {
			return nil, err
		}
		if p == "" {
			pw = "<contraseña desconocida>"
		} else {
			pw = p
		}
	}
	host := sv.Host
	if host == "" {
		host = "localhost"
	}
	db := in.Database
	var cs string
	switch {
	case sv.Engine == validate.EngineMSSQL:
		enc := "False"
		if sv.TLS || sv.Mode == "local" {
			enc = "True"
		}
		switch in.Format {
		case "dotnet":
			cs = fmt.Sprintf("Server=%s,%d;", host, sv.Port)
			if db != "" {
				cs += "Database=" + db + ";"
			}
			cs += fmt.Sprintf("User Id=%s;Password=%s;Encrypt=%s;TrustServerCertificate=True;", login, pw, enc)
		case "jdbc":
			cs = fmt.Sprintf("jdbc:sqlserver://%s:%d;", host, sv.Port)
			if db != "" {
				cs += "databaseName=" + db + ";"
			}
			cs += fmt.Sprintf("user=%s;password=%s;encrypt=%s;trustServerCertificate=true;", login, pw, strings.ToLower(enc))
		case "sqlcmd":
			cs = fmt.Sprintf("sqlcmd -S %s,%d -U %s -P %s -C", host, sv.Port, login, pw)
			if db != "" {
				cs += " -d " + db
			}
		default:
			return nil, errors.New("formato inválido para SQL Server: use dotnet, jdbc o sqlcmd")
		}
	case sv.Engine == validate.EnginePostgres:
		d := db
		if d == "" {
			d = "postgres"
		}
		switch in.Format {
		case "dotnet":
			cs = fmt.Sprintf("Host=%s;Port=%d;Database=%s;Username=%s;Password=%s;", host, sv.Port, d, login, pw)
		case "jdbc":
			cs = fmt.Sprintf("jdbc:postgresql://%s:%d/%s?user=%s&password=%s", host, sv.Port, d, login, pw)
		case "psql":
			cs = fmt.Sprintf("psql \"postgresql://%s:%s@%s:%d/%s\"", login, pw, host, sv.Port, d)
		default:
			return nil, errors.New("formato inválido para PostgreSQL: use dotnet, jdbc o psql")
		}
	default:
		return nil, errors.New("motor no soportado")
	}
	return map[string]any{"connectionString": cs, "login": login, "passwordIncluded": c.set.ExposePasswords}, nil
}
