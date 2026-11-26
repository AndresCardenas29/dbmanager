package validate

import (
	"strings"
	"testing"
)

func TestIdentifier(t *testing.T) {
	ok := []string{"a", "_x", "My-DB_1", "A1"}
	for _, n := range ok {
		if err := Identifier("mssql", n); err != nil {
			t.Errorf("%q debería ser válido: %v", n, err)
		}
	}
	bad := []string{"", "1abc", "a b", "a;drop", "a]b", "a'b", "-x", "ñandú", "a.b"}
	for _, n := range bad {
		if Identifier("mssql", n) == nil {
			t.Errorf("%q debería ser inválido", n)
		}
	}
	long := make([]byte, 64)
	for i := range long {
		long[i] = 'a'
	}
	if Identifier("postgres", string(long)) == nil {
		t.Error("64 chars debe fallar en postgres")
	}
	if Identifier("postgres", string(long[:63])) != nil {
		t.Error("63 chars debe pasar en postgres")
	}
	if Identifier("mssql", string(long)) != nil {
		t.Error("64 chars debe pasar en mssql")
	}
	if Identifier("mssql", strings.Repeat("a", 129)) == nil {
		t.Error("129 chars debe fallar")
	}
}

func TestQuoting(t *testing.T) {
	if got := MSSQLIdent("a]b"); got != "[a]]b]" {
		t.Errorf("got %s", got)
	}
	if got := MSSQLString("it's"); got != "N'it''s'" {
		t.Errorf("got %s", got)
	}
	if got := PGIdent(`we"ird`); got != `"we""ird"` {
		t.Errorf("got %s", got)
	}
	if got := PGString("o'k"); got != "'o''k'" {
		t.Errorf("got %s", got)
	}
}

func TestSQLServerPassword(t *testing.T) {
	good := []string{"Abcdef1!", "abcDEF12", "abc123!@#", "ABCdef!!"}
	for _, p := range good {
		if err := SQLServerPassword(p); err != nil {
			t.Errorf("%q debería pasar: %v", p, err)
		}
	}
	bad := []string{"", "Ab1!", "abcdefgh", "abcdefg1", "ABCDEFG1", "12345678"}
	for _, p := range bad {
		if err := SQLServerPassword(p); err == nil {
			t.Errorf("%q debería fallar", p)
		}
	}
	if err := SQLServerPassword("abc"); err == nil || err.Error() != "la contraseña debe tener al menos 8 caracteres" {
		t.Errorf("mensaje inesperado: %v", err)
	}
}
