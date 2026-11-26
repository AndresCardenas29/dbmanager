package postgres

import (
	"bytes"
	"io"
	"os"
	"strconv"
)

// Formatos de backup reconocidos.
const (
	FormatCustom = "custom" // pg_dump -Fc (magic PGDMP) -> pg_restore
	FormatPlain  = "plain"  // SQL -> psql
)

// DetectFormat devuelve el formato según los primeros bytes del archivo.
func DetectFormat(head []byte) string {
	if bytes.HasPrefix(head, []byte("PGDMP")) {
		return FormatCustom
	}
	return FormatPlain
}

// DetectFormatFile lee los primeros bytes del archivo y detecta el formato.
func DetectFormatFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, 5)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	return DetectFormat(buf[:n]), nil
}

// ConnArgs son los argumentos de conexión para las herramientas cliente.
// host vacío = socket local (dentro del contenedor).
func ConnArgs(host string, port int, user string) []string {
	args := []string{"-U", user}
	if host != "" {
		args = append(args, "-h", host, "-p", strconv.Itoa(port))
	}
	return args
}

// DumpCmd arma `pg_dump -Fc`.
func DumpCmd(host string, port int, user, database, outFile string) []string {
	cmd := []string{"pg_dump", "-Fc", "--no-password"}
	cmd = append(cmd, ConnArgs(host, port, user)...)
	return append(cmd, "-f", outFile, database)
}

// RestoreCmd arma pg_restore (custom) o psql (plain) hacia la base destino.
func RestoreCmd(format, host string, port int, user, target, file string) []string {
	if format == FormatCustom {
		cmd := []string{"pg_restore", "--no-password", "--no-owner", "--no-privileges"}
		cmd = append(cmd, ConnArgs(host, port, user)...)
		return append(cmd, "-d", target, file)
	}
	cmd := []string{"psql", "--no-password", "-v", "ON_ERROR_STOP=1", "-q"}
	cmd = append(cmd, ConnArgs(host, port, user)...)
	return append(cmd, "-d", target, "-f", file)
}

// MajorFromVersion extrae la versión mayor de "PostgreSQL 16.4 ..." o "16.4"; 0 si no se reconoce.
func MajorFromVersion(v string) int {
	n, started := 0, false
	for _, r := range v {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
			started = true
		} else if started {
			break
		}
	}
	return n
}
