package sftp

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"time"

	psftp "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// LoginRequest son los datos de una prueba de acceso.
type LoginRequest struct {
	Host           string
	Port           int
	User           string
	Password       string
	PrivateKeyPath string
	Passphrase     string
	// KnownFingerprints (opcional): si se indica, la huella del servidor debe estar entre ellas.
	KnownFingerprints []string
}

// LoginResult es el resultado de TestLogin.
type LoginResult struct {
	OK              bool
	Message         string
	HostFingerprint string
	Entries         []string
}

// LoadSigner lee una llave privada OpenSSH (con frase de paso opcional).
func LoadSigner(path, passphrase string) (ssh.Signer, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no se puede leer la llave privada: %w", err)
	}
	s, err := ssh.ParsePrivateKey(b)
	if err == nil {
		return s, nil
	}
	var pm *ssh.PassphraseMissingError
	if errors.As(err, &pm) {
		if passphrase == "" {
			return nil, errors.New("la llave privada tiene frase de paso: indíquela")
		}
		s, err := ssh.ParsePrivateKeyWithPassphrase(b, []byte(passphrase))
		if err != nil {
			return nil, errors.New("frase de paso incorrecta")
		}
		return s, nil
	}
	return nil, fmt.Errorf("la llave privada no es válida (se espera formato OpenSSH/PEM): %w", err)
}

// TestLogin hace un login SSH/SFTP real y lista el directorio raíz (el home chroot del usuario).
func TestLogin(r LoginRequest) LoginResult {
	var res LoginResult
	var auth []ssh.AuthMethod
	if r.PrivateKeyPath != "" {
		s, err := LoadSigner(r.PrivateKeyPath, r.Passphrase)
		if err != nil {
			res.Message = err.Error()
			return res
		}
		auth = append(auth, ssh.PublicKeys(s))
	}
	if r.Password != "" {
		auth = append(auth, ssh.Password(r.Password))
	}
	if len(auth) == 0 {
		res.Message = "indique una contraseña o una llave privada"
		return res
	}
	var hostErr error
	cfg := &ssh.ClientConfig{
		User: r.User, Auth: auth, Timeout: 10 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			res.HostFingerprint = ssh.FingerprintSHA256(k)
			if len(r.KnownFingerprints) > 0 {
				for _, f := range r.KnownFingerprints {
					if f == res.HostFingerprint {
						return nil
					}
				}
				hostErr = errors.New("la huella del servidor no coincide con la registrada")
				return hostErr
			}
			return nil
		},
	}
	addr := net.JoinHostPort(r.Host, strconv.Itoa(r.Port))
	var conn *ssh.Client
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		conn, err = ssh.Dial("tcp", addr, cfg)
		if err == nil || !transient(err) || hostErr != nil {
			break
		}
		time.Sleep(time.Duration(400*(attempt+1)) * time.Millisecond) // OpenSSH 9.8+ corta la conexión tras intentos fallidos
	}
	if err != nil {
		switch {
		case hostErr != nil:
			res.Message = hostErr.Error() + " (" + res.HostFingerprint + ")"
		default:
			res.Message = "No se pudo iniciar sesión: " + friendlySSH(err)
		}
		return res
	}
	defer conn.Close()
	cl, err := psftp.NewClient(conn)
	if err != nil {
		res.Message = "Sesión SSH correcta, pero falló el subsistema SFTP: " + err.Error()
		return res
	}
	defer cl.Close()
	infos, err := cl.ReadDir("/")
	if err != nil {
		res.Message = "No se pudo listar el directorio: " + err.Error()
		return res
	}
	for _, fi := range infos {
		n := fi.Name()
		if fi.IsDir() {
			n += "/"
		}
		res.Entries = append(res.Entries, n)
	}
	sort.Strings(res.Entries)
	res.OK = true
	res.Message = "Acceso correcto."
	return res
}

func friendlySSH(err error) string {
	m := err.Error()
	switch {
	case containsAny(m, "unable to authenticate", "no supported methods remain"):
		return "usuario, contraseña o llave incorrectos (" + m + ")"
	case transient(err):
		return "el servidor cerró la conexión: suele ocurrir con usuario, contraseña o llave incorrectos, o por un bloqueo temporal tras varios intentos fallidos (" + m + ")"
	case containsAny(m, "connection refused", "actively refused"):
		return "conexión rechazada: ¿el servicio está iniciado? (" + m + ")"
	case containsAny(m, "i/o timeout", "timed out"):
		return "tiempo de espera agotado (" + m + ")"
	}
	return m
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		for i := 0; i+len(x) <= len(s); i++ {
			if s[i:i+len(x)] == x {
				return true
			}
		}
	}
	return false
}

// ScanHostKeys obtiene las llaves públicas de host que ofrece un servidor (ed25519 y rsa).
func ScanHostKeys(host string, port int) ([]ssh.PublicKey, error) {
	var out []ssh.PublicKey
	var lastErr error
	for _, algo := range []string{ssh.KeyAlgoED25519, ssh.KeyAlgoRSASHA512} {
		var got ssh.PublicKey
		cfg := &ssh.ClientConfig{
			User: "probe", Timeout: 5 * time.Second, HostKeyAlgorithms: []string{algo},
			HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
				got = k
				return errors.New("sonda de llave de host")
			},
		}
		c, err := ssh.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)), cfg)
		if c != nil {
			c.Close()
		}
		if got != nil {
			out = append(out, got)
		} else if err != nil {
			lastErr = err
		}
	}
	if len(out) == 0 {
		if lastErr == nil {
			lastErr = errors.New("sin respuesta")
		}
		return nil, fmt.Errorf("no se pudo obtener la llave del servidor: %w", lastErr)
	}
	return out, nil
}

// transient indica errores de red pasajeros (EOF / reset) que justifican reintentar.
func transient(err error) bool {
	return err != nil && containsAny(err.Error(), "EOF", "connection reset", "broken pipe")
}
