// Package validate agrupa validación de nombres, quoting seguro y política de contraseñas.
package validate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_\-]{0,127}$`)

const (
	EngineMSSQL    = "mssql"
	EnginePostgres = "postgres"
)

// Identifier valida un nombre de BD/usuario para el motor dado.
func Identifier(engine, name string) error {
	if !identRe.MatchString(name) {
		return errors.New("nombre inválido: use letras, números, guion o guion bajo; debe empezar con letra o guion bajo (máx. 128 caracteres)")
	}
	if engine == EnginePostgres && len(name) > 63 {
		return errors.New("nombre inválido: PostgreSQL permite máximo 63 caracteres")
	}
	return nil
}

// MSSQLIdent devuelve el identificador entre corchetes, duplicando ']'.
func MSSQLIdent(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

// MSSQLString devuelve un literal Unicode N'...' escapando comillas simples.
func MSSQLString(s string) string {
	return "N'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// PGIdent devuelve el identificador PostgreSQL citado de forma segura.
func PGIdent(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

// PGString devuelve un literal de cadena PostgreSQL (escapa ' y \ usando E” no necesario con standard_conforming_strings).
func PGString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// SQLServerPassword valida la política de SQL Server: >= 8 caracteres y 3 de 4 categorías.
func SQLServerPassword(pw string) error {
	if len([]rune(pw)) < 8 {
		return errors.New("la contraseña debe tener al menos 8 caracteres")
	}
	var up, low, dig, sym bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			up = true
		case unicode.IsLower(r):
			low = true
		case unicode.IsDigit(r):
			dig = true
		default:
			sym = true
		}
	}
	n := 0
	for _, b := range []bool{up, low, dig, sym} {
		if b {
			n++
		}
	}
	if n < 3 {
		return fmt.Errorf("la contraseña debe combinar al menos 3 de estas 4 categorías: mayúsculas, minúsculas, números y símbolos")
	}
	return nil
}
