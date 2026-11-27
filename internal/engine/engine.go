// Package engine define la interfaz común de los motores (SQL Server, PostgreSQL).
package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Niveles de acceso por base de datos.
const (
	LevelOwner     = "owner"
	LevelReadWrite = "readwrite"
	LevelReadOnly  = "readonly"
	LevelNone      = "none"
	LevelCustom    = "custom" // acceso a la base sin roles globales: solo permisos por esquema/tabla
)

// Privileges son los privilegios gestionados por esquema/tabla, en este orden.
var Privileges = []string{"SELECT", "INSERT", "UPDATE", "DELETE", "EXECUTE", "ALTER"}

// Schema es un esquema de una base de datos con sus tablas/vistas.
type Schema struct {
	Name   string
	Owner  string
	Tables []string
	System bool
}

// ObjectGrant son los privilegios gestionados de un login sobre un esquema (Table == "") o una tabla.
type ObjectGrant struct {
	Login      string
	Schema     string
	Table      string // "" = esquema completo
	Privileges []string
}

// NormalizePrivileges valida (sin distinguir mayúsculas), pasa a mayúsculas, quita duplicados y ordena
// según Privileges. Si table es true, EXECUTE no es válido.
func NormalizePrivileges(in []string, table bool) ([]string, error) {
	set := map[string]bool{}
	for _, p := range in {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		ok := false
		for _, a := range Privileges {
			if a == p {
				ok = true
			}
		}
		if !ok {
			return nil, fmt.Errorf("privilegio no permitido: %s", p)
		}
		if table && p == "EXECUTE" {
			return nil, errors.New("EXECUTE solo se puede otorgar a nivel de esquema, no sobre una tabla")
		}
		set[p] = true
	}
	out := []string{}
	for _, a := range Privileges {
		if set[a] {
			out = append(out, a)
		}
	}
	return out, nil
}

// ValidLevel indica si level es un nivel de acceso válido.
func ValidLevel(l string) bool {
	switch l {
	case LevelOwner, LevelReadWrite, LevelReadOnly, LevelNone, LevelCustom:
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
	// IsAdmin indica si el usuario conectado es administrador total (sysadmin / SUPERUSER).
	IsAdmin(ctx context.Context) (bool, error)
	// EnsureAdminLogin crea (o actualiza) un login con permisos de administrador total y la contraseña dada.
	// Es la operación que usa DB Manager para su usuario "root"; admite el login conectado.
	EnsureAdminLogin(ctx context.Context, name, password string) error
	// ReassignOwnership pasa a "to" lo que posee "from" para poder eliminar "from".
	ReassignOwnership(ctx context.Context, from, to string) error
	// ListSchemas lista los esquemas de usuario de una base con sus tablas y vistas.
	ListSchemas(ctx context.Context, database string) ([]Schema, error)
	CreateSchema(ctx context.Context, database, name string) error
	// ListObjectGrants devuelve los privilegios gestionados de los logins no-sistema por esquema/tabla.
	ListObjectGrants(ctx context.Context, database string) ([]ObjectGrant, error)
	// SetObjectGrant deja EXACTAMENTE ese conjunto de privilegios gestionados (otorga los que faltan, revoca los sobrantes).
	SetObjectGrant(ctx context.Context, database, login, schema, table string, privileges []string) error
	Close() error
}
