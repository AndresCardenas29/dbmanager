// Package engine define la interfaz común de los motores (SQL Server, PostgreSQL).
package engine

import "context"

// Niveles de acceso por base de datos.
const (
	LevelOwner     = "owner"
	LevelReadWrite = "readwrite"
	LevelReadOnly  = "readonly"
	LevelNone      = "none"
)

// ValidLevel indica si level es un nivel de acceso válido.
func ValidLevel(l string) bool {
	switch l {
	case LevelOwner, LevelReadWrite, LevelReadOnly, LevelNone:
		return true
	}
	return false
}

// Conn son los datos para conectarse a un servidor.
type Conn struct {
	Host      string
	Port      int
	User      string
	Password  string
	TLS       bool // exigir cifrado
	TrustCert bool // aceptar certificado no confiable
}

type Database struct {
	Name   string
	SizeMB float64
	Owner  string
	State  string
	System bool
}

type Login struct {
	Name        string
	ServerRoles []string
	Disabled    bool
	IsSystem    bool
}

type Cell struct {
	Login    string
	Database string
	Level    string
}

type Matrix struct {
	Logins    []string
	Databases []string
	Cells     []Cell
}

type BackupFile struct {
	LogicalName  string
	PhysicalName string
	Type         string // data | log
}

type BackupInfo struct {
	Format       string
	DatabaseName string
	Files        []BackupFile
}

// ProgressFunc recibe porcentaje (0-100) y mensaje.
type ProgressFunc func(percent int, message string)

// Engine agrupa las operaciones SQL comunes. Los backups/restauraciones dependen del motor
// y se orquestan en el paquete services.
type Engine interface {
	// Ping verifica la conexión y devuelve la versión del servidor.
	Ping(ctx context.Context) (string, error)
	ListDatabases(ctx context.Context) ([]Database, error)
	DatabaseExists(ctx context.Context, name string) (bool, error)
	CreateDatabase(ctx context.Context, name, owner string) error
	DropDatabase(ctx context.Context, name string) error
	ListLogins(ctx context.Context) ([]Login, error)
	CreateLogin(ctx context.Context, name, password string, serverRoles []string) error
	DropLogin(ctx context.Context, name string) error
	ChangePassword(ctx context.Context, name, password string) error
	SetServerRoles(ctx context.Context, name string, roles []string) error
	GetPermissionMatrix(ctx context.Context) (Matrix, error)
	SetDatabaseAccess(ctx context.Context, login, database, level string) error
	Close() error
}
