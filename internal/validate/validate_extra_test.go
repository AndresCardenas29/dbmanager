package validate

import (
	"math/rand"
	"strings"
	"testing"
)

// Cadenas hostiles para cualquier identificador o literal.
var hostile = []string{
	"a]b", "a]]b", "]", "]]]", "[", "x'; DROP DATABASE master; --", "'", "''", "\\", `\'`, "a\\", `"`, `a"b`, `"; DROP ROLE x; --`,
	"ñandú", "日本語", "😀", "a\x00b", "a\nb", "a\r\nb", "  ", "", "--", "/*", "*/", "%s", "$1", "a;b", "N'x'", "\u2028",
}

func unwrapMSSQLIdent(t *testing.T, q string) string {
	t.Helper()
	if !strings.HasPrefix(q, "[") || !strings.HasSuffix(q, "]") {
		t.Fatalf("sin corchetes: %q", q)
	}
	body := q[1 : len(q)-1]
	// dentro, todo ']' debe aparecer en pares
	var out strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] == ']' {
			if i+1 >= len(body) || body[i+1] != ']' {
				t.Fatalf("']' sin escapar en %q", q)
			}
			i++
		}
		out.WriteByte(body[i])
	}
	return out.String()
}

func unwrapQuoted(t *testing.T, q, prefix string, quote byte) string {
	t.Helper()
	if !strings.HasPrefix(q, prefix+string(quote)) || q[len(q)-1] != quote {
		t.Fatalf("formato inesperado: %q", q)
	}
	body := q[len(prefix)+1 : len(q)-1]
	var out strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] == quote {
			if i+1 >= len(body) || body[i+1] != quote {
				t.Fatalf("comilla sin escapar en %q", q)
			}
			i++
		}
		out.WriteByte(body[i])
	}
	return out.String()
}

func TestQuotingRoundTripHostile(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	inputs := append([]string{}, hostile...)
	alphabet := []rune("ab]'\"\\;-[ ñ\n")
	for i := 0; i < 2000; i++ {
		n := rnd.Intn(12)
		rs := make([]rune, n)
		for j := range rs {
			rs[j] = alphabet[rnd.Intn(len(alphabet))]
		}
		inputs = append(inputs, string(rs))
	}
	for _, in := range inputs {
		if got := unwrapMSSQLIdent(t, MSSQLIdent(in)); got != in {
			t.Errorf("MSSQLIdent(%q) no es reversible: %q", in, got)
		}
		if got := unwrapQuoted(t, MSSQLString(in), "N", '\''); got != in {
			t.Errorf("MSSQLString(%q) no es reversible: %q", in, got)
		}
		if got := unwrapQuoted(t, PGIdent(in), "", '"'); got != in {
			// pgx.Identifier elimina NUL; lo aceptamos solo en ese caso
			if strings.ContainsRune(in, 0) && got == strings.ReplaceAll(in, "\x00", "") {
				continue
			}
			t.Errorf("PGIdent(%q) no es reversible: %q", in, got)
		}
		if got := unwrapQuoted(t, PGString(in), "", '\''); got != in {
			t.Errorf("PGString(%q) no es reversible: %q", in, got)
		}
	}
}

// Ningún nombre hostil debe pasar la validación de identificadores.
func TestIdentifierRejectsHostile(t *testing.T) {
	for _, h := range hostile {
		for _, eng := range []string{EngineMSSQL, EnginePostgres} {
			if h != "" && Identifier(eng, h) == nil {
				t.Errorf("%s: %q no debería ser válido", eng, h)
			}
			if h == "" && Identifier(eng, h) == nil {
				t.Errorf("%s: vacío no debería ser válido", eng)
			}
		}
	}
	// saltos de línea finales: en Go `$` no coincide antes de un \n final, pero lo comprobamos.
	for _, n := range []string{"abc\n", "abc\r\n", "abc\x00", "\nabc", "abc ", " abc"} {
		if Identifier(EngineMSSQL, n) == nil {
			t.Errorf("%q no debería ser válido", n)
		}
	}
}

func TestIdentifierBoundaries(t *testing.T) {
	for _, tc := range []struct {
		eng  string
		n    int
		want bool
	}{
		{EngineMSSQL, 1, true}, {EngineMSSQL, 128, true}, {EngineMSSQL, 129, false},
		{EnginePostgres, 1, true}, {EnginePostgres, 63, true}, {EnginePostgres, 64, false}, {EnginePostgres, 128, false},
	} {
		if got := Identifier(tc.eng, strings.Repeat("a", tc.n)) == nil; got != tc.want {
			t.Errorf("%s len=%d: ok=%v, esperaba %v", tc.eng, tc.n, got, tc.want)
		}
	}
	// el mensaje de error está en español
	err := Identifier(EngineMSSQL, "a]b")
	if err == nil || !strings.Contains(err.Error(), "nombre inválido") {
		t.Errorf("mensaje: %v", err)
	}
}

func TestSQLServerPasswordCountsRunes(t *testing.T) {
	// 7 runas (9 bytes en UTF-8): debe fallar por longitud
	if err := SQLServerPassword("Añ1!ñññ"); err == nil {
		t.Error("7 runas debe fallar")
	}
	if err := SQLServerPassword("Añ1!ñññx"); err != nil {
		t.Errorf("8 runas con 4 categorías debe pasar: %v", err)
	}
}
