package sftp

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/GehirnInc/crypt"
	_ "github.com/GehirnInc/crypt/sha512_crypt"
	"golang.org/x/crypto/ssh"
)

// GID por defecto: grupo "users" de la imagen atmoz.
const DefaultGID = 100

var (
	userRe      = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	uploadDirRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)
)

// ValidUserName valida el nombre de usuario (nombre de usuario de Linux en minúsculas).
func ValidUserName(n string) error {
	if !userRe.MatchString(n) {
		return errors.New("nombre de usuario inválido: use minúsculas, números, guion o guion bajo (empieza con letra o _; máx. 32)")
	}
	switch n {
	case "root", "sshd", "nobody", "daemon", "bin", "sys":
		return errors.New("nombre de usuario reservado")
	}
	return nil
}

// ValidUploadDir valida la subcarpeta de carga (un solo nivel).
func ValidUploadDir(d string) error {
	if !uploadDirRe.MatchString(d) || d == "." || d == ".." {
		return errors.New("carpeta de carga inválida: un solo nombre sin barras (letras, números, _ . -)")
	}
	return nil
}

// HashPassword devuelve un hash SHA-512 crypt ($6$...) para users.conf (formato ":e" de atmoz).
func HashPassword(pw string) (string, error) {
	const alphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	salt := make([]byte, 16)
	for i := range salt {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		salt[i] = alphabet[n.Int64()]
	}
	return crypt.SHA512.New().Generate([]byte(pw), append([]byte("$6$"), salt...))
}

// VerifyPassword comprueba una contraseña contra un hash crypt (útil en pruebas).
func VerifyPassword(pw, hash string) bool {
	return crypt.SHA512.New().Verify(hash, []byte(pw)) == nil
}

// UserCfg es un usuario para generar la configuración.
type UserCfg struct {
	Name         string
	UID          int
	PasswordHash string // "" = solo llave
	UploadDir    string
	PublicKeys   []string // líneas authorized_keys
}

// UsersConf genera users.conf (formato atmoz: user:pass[:e]:uid:gid:dirs).
func UsersConf(users []UserCfg) string {
	var sb strings.Builder
	for _, u := range users {
		if u.PasswordHash == "" {
			fmt.Fprintf(&sb, "%s::%d:%d:%s\n", u.Name, u.UID, DefaultGID, u.UploadDir)
		} else {
			fmt.Fprintf(&sb, "%s:%s:e:%d:%d:%s\n", u.Name, u.PasswordHash, u.UID, DefaultGID, u.UploadDir)
		}
	}
	return sb.String()
}

// HookScript genera /etc/sftp.d/10-persist-host-keys.sh: restaura las llaves de host persistidas
// (la app las genera una vez) y ajusta propietarios de las carpetas de carga. Líneas LF.
func HookScript(users []UserCfg) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n# Generado por DB Manager. No editar.\n")
	sb.WriteString("if ls /etc/sftp-keys/ssh_host_* >/dev/null 2>&1; then\n  cp /etc/sftp-keys/ssh_host_* /etc/ssh/\nelse\n  cp /etc/ssh/ssh_host_* /etc/sftp-keys/ 2>/dev/null || true\nfi\n")
	sb.WriteString("chmod 600 /etc/ssh/ssh_host_*_key 2>/dev/null || true\nchmod 644 /etc/ssh/ssh_host_*.pub 2>/dev/null || true\n")
	for _, u := range users {
		fmt.Fprintf(&sb, "chown -R %d:%d \"/home/%s/%s\" 2>/dev/null || true\n", u.UID, DefaultGID, u.Name, u.UploadDir)
	}
	return sb.String()
}

// EnsureHostKeys genera (una sola vez) las llaves de host ed25519 y rsa en dir.
func EnsureHostKeys(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	type spec struct {
		name string
		gen  func() (any, ssh.PublicKey, error)
	}
	specs := []spec{
		{"ssh_host_ed25519_key", func() (any, ssh.PublicKey, error) {
			pub, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return nil, nil, err
			}
			sp, err := ssh.NewPublicKey(pub)
			return priv, sp, err
		}},
		{"ssh_host_rsa_key", func() (any, ssh.PublicKey, error) {
			k, err := rsa.GenerateKey(rand.Reader, 3072)
			if err != nil {
				return nil, nil, err
			}
			sp, err := ssh.NewPublicKey(&k.PublicKey)
			return k, sp, err
		}},
	}
	for _, s := range specs {
		priv := filepath.Join(dir, s.name)
		if _, err := os.Stat(priv); err == nil {
			continue
		}
		k, pub, err := s.gen()
		if err != nil {
			return err
		}
		blk, err := ssh.MarshalPrivateKey(k, "")
		if err != nil {
			return err
		}
		if err := os.WriteFile(priv, pem.EncodeToMemory(blk), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(priv+".pub", []byte(AuthorizedLine(pub, "")+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// HostFingerprints devuelve las huellas "ED25519 SHA256:..." de las llaves de host de dir.
func HostFingerprints(dir string) ([]string, error) {
	var out []string
	for _, n := range []struct{ file, label string }{{"ssh_host_ed25519_key.pub", "ED25519"}, {"ssh_host_rsa_key.pub", "RSA"}} {
		b, err := os.ReadFile(filepath.Join(dir, n.file))
		if err != nil {
			continue
		}
		pub, _, _, _, err := ssh.ParseAuthorizedKey(b)
		if err != nil {
			continue
		}
		out = append(out, n.label+" "+ssh.FingerprintSHA256(pub))
	}
	if len(out) == 0 {
		return nil, errors.New("el servicio no tiene llaves de host")
	}
	return out, nil
}

// WriteConfig escribe users.conf, hooks y llaves públicas de los usuarios bajo base. Devuelve rutas.
type ConfigPaths struct {
	UsersConf string
	HooksDir  string
	KeysDir   string // keys/<user>/*.pub
	HostKeys  string
}

func Paths(base string) ConfigPaths {
	return ConfigPaths{UsersConf: filepath.Join(base, "users.conf"), HooksDir: filepath.Join(base, "hooks"),
		KeysDir: filepath.Join(base, "keys"), HostKeys: filepath.Join(base, "hostkeys")}
}
