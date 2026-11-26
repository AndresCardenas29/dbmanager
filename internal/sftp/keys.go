// Package sftp gestiona servicios SFTP en Docker (atmoz/sftp): configuración, llaves de acceso y pruebas de login.
package sftp

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Tipos de llave soportados.
const (
	KeyEd25519 = "ed25519"
	KeyRSA4096 = "rsa4096"
)

// GeneratedKey es una llave recién generada. La privada no se almacena en ningún sitio por la app:
// quien llama decide dónde escribirla.
type GeneratedKey struct {
	Private     crypto.PrivateKey
	PrivatePEM  []byte // formato OpenSSH (cifrado si hubo frase de paso)
	PublicLine  string // "ssh-ed25519 AAAA... comentario"
	Fingerprint string // SHA256:...
	Type        string // ED25519 | RSA 4096
	Comment     string
}

// Generate crea una llave. keyType: ed25519 (por defecto) o rsa4096.
func Generate(keyType, passphrase, comment string) (GeneratedKey, error) {
	if err := ValidComment(comment); err != nil {
		return GeneratedKey{}, err
	}
	var priv crypto.PrivateKey
	var pub crypto.PublicKey
	switch keyType {
	case "", KeyEd25519:
		p, k, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return GeneratedKey{}, err
		}
		priv, pub = k, p
	case KeyRSA4096:
		k, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			return GeneratedKey{}, err
		}
		priv, pub = k, &k.PublicKey
	default:
		return GeneratedKey{}, fmt.Errorf("tipo de llave no soportado: %q (use ed25519 o rsa4096)", keyType)
	}
	var blk *pem.Block
	var err error
	if passphrase != "" {
		blk, err = ssh.MarshalPrivateKeyWithPassphrase(priv, comment, []byte(passphrase))
	} else {
		blk, err = ssh.MarshalPrivateKey(priv, comment)
	}
	if err != nil {
		return GeneratedKey{}, err
	}
	spub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return GeneratedKey{}, err
	}
	return GeneratedKey{Private: priv, PrivatePEM: pem.EncodeToMemory(blk), PublicLine: AuthorizedLine(spub, comment),
		Fingerprint: ssh.FingerprintSHA256(spub), Type: KeyLabel(spub), Comment: comment}, nil
}

// AuthorizedLine devuelve la línea de authorized_keys/.pub sin salto de línea.
func AuthorizedLine(pub ssh.PublicKey, comment string) string {
	l := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment = cleanComment(comment); comment != "" {
		l += " " + comment
	}
	return l
}

// KeyLabel describe el tipo de llave pública ("ED25519", "RSA 4096", ...).
func KeyLabel(pub ssh.PublicKey) string {
	if c, ok := pub.(ssh.CryptoPublicKey); ok {
		if r, ok := c.CryptoPublicKey().(*rsa.PublicKey); ok {
			return fmt.Sprintf("RSA %d", r.N.BitLen())
		}
	}
	switch pub.Type() {
	case ssh.KeyAlgoED25519:
		return "ED25519"
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		return "ECDSA"
	}
	return pub.Type()
}

// ParsePublicKey valida una llave pública pegada (una sola línea, sin opciones de authorized_keys).
// Devuelve la línea canónica, huella, tipo y comentario.
func ParsePublicKey(line string) (canon, fingerprint, typ, comment string, err error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", "", "", errors.New("pegue una llave pública (formato OpenSSH: ssh-ed25519 AAAA... comentario)")
	}
	if strings.ContainsAny(line, "\r\n") {
		return "", "", "", "", errors.New("pegue una sola llave pública (una línea)")
	}
	pub, cmt, opts, _, perr := ssh.ParseAuthorizedKey([]byte(line))
	if perr != nil {
		return "", "", "", "", errors.New("la llave pública no es válida (use el formato OpenSSH: ssh-ed25519 AAAA... o ssh-rsa AAAA...)")
	}
	if len(opts) > 0 {
		return "", "", "", "", errors.New("la llave pública no debe incluir opciones (command=, from=, ...)")
	}
	return AuthorizedLine(pub, cmt), ssh.FingerprintSHA256(pub), KeyLabel(pub), cmt, nil
}

// ValidComment rechaza comentarios con caracteres de control o demasiado largos (evita inyección en authorized_keys).
func ValidComment(c string) error {
	if len(c) > 200 {
		return errors.New("el comentario de la llave es demasiado largo (máx. 200 caracteres)")
	}
	for _, r := range c {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x2028 || r == 0x2029 {
			return errors.New("el comentario de la llave no puede contener saltos de línea, tabuladores ni otros caracteres de control")
		}
	}
	return nil
}

// cleanComment reemplaza caracteres de control por espacio (defensa adicional al escribir .pub/authorized_keys).
func cleanComment(c string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x2028 || r == 0x2029 {
			return ' '
		}
		return r
	}, c))
}
