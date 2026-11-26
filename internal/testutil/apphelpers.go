package testutil

import (
	"fmt"
	"regexp"
	"strings"
)

var englishWords = regexp.MustCompile(`(?i)\b(failed|cannot|can't|could not|unable|invalid|not found|does not|doesn't|unexpected|permission denied|connection refused|no such|already exists|login failed|is not|are not|must be|required|timeout|timed out)\b`)

// LooksEnglish indica si un mensaje parece estar (parcialmente) en inglés. Heurística para reportar,
// no para bloquear: los mensajes crudos del servidor SQL/Docker suelen venir en inglés.
func LooksEnglish(msg string) bool { return englishWords.MatchString(msg) }

// HasSpanish indica si el mensaje contiene algún marcador típico del español (tildes, ñ o palabras comunes).
func HasSpanish(msg string) bool {
	m := strings.ToLower(msg)
	if strings.ContainsAny(m, "áéíóúñ¿¡") {
		return true
	}
	for _, w := range []string{" no ", "el ", "la ", "los ", "las ", "se ", " de ", " ya ", "debe", "servidor", "base de datos", "usuario", "contraseña", "puerto", "contenedor", "elija", "indique", "seleccione"} {
		if strings.Contains(" "+m, w) {
			return true
		}
	}
	return false
}

// Must formatea errores de preparación.
func Must(err error, what string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}
