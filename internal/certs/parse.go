// Package certs lee llaves y certificados (PEM/DER), valida cadenas y construye/verifica PKCS#12.
package certs

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/youmark/pkcs8"
)

// ErrPassphraseRequired indica que la llave está cifrada y no se dio frase de paso.
var ErrPassphraseRequired = errors.New("la llave privada está cifrada: indique la frase de paso")

// ErrBadPassphrase indica frase de paso incorrecta.
var ErrBadPassphrase = errors.New("frase de paso incorrecta")

// Summary resume un certificado.
type Summary struct {
	Subject    string
	Issuer     string
	CommonName string
	SANs       []string
	NotBefore  time.Time
	NotAfter   time.Time
	Serial     string
	IsCA       bool
	SelfSigned bool
	SHA256     string
}

// IsSelfSigned indica si el certificado está firmado por sí mismo.
func IsSelfSigned(c *x509.Certificate) bool {
	return c.Subject.String() == c.Issuer.String() && c.CheckSignatureFrom(c) == nil
}

// Summarize resume un certificado.
func Summarize(c *x509.Certificate) Summary {
	sans := []string{}
	sans = append(sans, c.DNSNames...)
	for _, ip := range c.IPAddresses {
		sans = append(sans, ip.String())
	}
	sans = append(sans, c.EmailAddresses...)
	sum := sha256.Sum256(c.Raw)
	return Summary{
		Subject: c.Subject.String(), Issuer: c.Issuer.String(), CommonName: c.Subject.CommonName, SANs: sans,
		NotBefore: c.NotBefore, NotAfter: c.NotAfter, Serial: c.SerialNumber.Text(16), IsCA: c.IsCA,
		SelfSigned: IsSelfSigned(c), SHA256: strings.ToUpper(hex.EncodeToString(sum[:])),
	}
}

// KeyType describe el tipo y tamaño de una llave.
func KeyType(k any) string {
	switch v := k.(type) {
	case *rsa.PrivateKey:
		return fmt.Sprintf("RSA %d", v.N.BitLen())
	case *ecdsa.PrivateKey:
		return "ECDSA " + v.Curve.Params().Name
	case ed25519.PrivateKey:
		return "Ed25519"
	}
	return fmt.Sprintf("%T", k)
}

func parseDERKey(der []byte) (crypto.PrivateKey, bool) {
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		return k, true
	}
	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, true
	}
	if k, err := x509.ParseECPrivateKey(der); err == nil {
		return k, true
	}
	return nil, false
}

// KeyInfo es el resultado de leer un archivo de llave.
type KeyInfo struct {
	Key       crypto.PrivateKey
	Type      string
	Encrypted bool
}

// LoadKey lee una llave privada (PEM PKCS#1/PKCS#8/EC, cifrada o no, o DER). Si está cifrada
// y falta la frase se devuelve ErrPassphraseRequired (con Encrypted=true en el KeyInfo).
func LoadKey(path, pass string) (KeyInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("no se puede leer la llave privada: %w", err)
	}
	return ParseKey(data, pass)
}

// ParseKey es LoadKey sobre bytes.
func ParseKey(data []byte, pass string) (KeyInfo, error) {
	rest := data
	foundPEM := false
	for {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		foundPEM = true
		switch {
		case b.Type == "ENCRYPTED PRIVATE KEY":
			if pass == "" {
				return KeyInfo{Encrypted: true}, ErrPassphraseRequired
			}
			k, err := pkcs8.ParsePKCS8PrivateKey(b.Bytes, []byte(pass))
			if err != nil {
				return KeyInfo{Encrypted: true}, ErrBadPassphrase
			}
			return KeyInfo{Key: k, Type: KeyType(k), Encrypted: true}, nil
		case strings.HasSuffix(b.Type, "PRIVATE KEY") && x509.IsEncryptedPEMBlock(b): //nolint:staticcheck // formato legado Proc-Type 4,ENCRYPTED
			if pass == "" {
				return KeyInfo{Encrypted: true}, ErrPassphraseRequired
			}
			der, err := x509.DecryptPEMBlock(b, []byte(pass)) //nolint:staticcheck
			if err != nil {
				return KeyInfo{Encrypted: true}, ErrBadPassphrase
			}
			k, ok := parseDERKey(der)
			if !ok {
				return KeyInfo{Encrypted: true}, ErrBadPassphrase
			}
			return KeyInfo{Key: k, Type: KeyType(k), Encrypted: true}, nil
		case strings.HasSuffix(b.Type, "PRIVATE KEY"):
			if k, ok := parseDERKey(b.Bytes); ok {
				return KeyInfo{Key: k, Type: KeyType(k)}, nil
			}
			return KeyInfo{}, fmt.Errorf("no se pudo interpretar la llave privada (%s)", b.Type)
		}
	}
	if foundPEM {
		return KeyInfo{}, errors.New("el archivo PEM no contiene una llave privada")
	}
	if k, ok := parseDERKey(data); ok {
		return KeyInfo{Key: k, Type: KeyType(k)}, nil
	}
	// PKCS#8 cifrado en DER
	if pass != "" {
		if k, err := pkcs8.ParsePKCS8PrivateKey(data, []byte(pass)); err == nil {
			return KeyInfo{Key: k, Type: KeyType(k), Encrypted: true}, nil
		}
	}
	return KeyInfo{}, errors.New("el archivo no es una llave privada reconocible (PEM PKCS#1/PKCS#8/EC o DER)")
}

// LoadCerts lee uno o varios certificados de un archivo PEM o DER.
func LoadCerts(path string) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no se puede leer el certificado: %w", err)
	}
	return ParseCerts(data)
}

// ParseCerts es LoadCerts sobre bytes.
func ParseCerts(data []byte) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	rest := data
	foundPEM := false
	for {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		foundPEM = true
		if b.Type != "CERTIFICATE" && b.Type != "TRUSTED CERTIFICATE" && b.Type != "X509 CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			return nil, fmt.Errorf("certificado inválido en el archivo: %w", err)
		}
		out = append(out, c)
	}
	if len(out) == 0 && !foundPEM {
		cs, err := x509.ParseCertificates(data)
		if err != nil {
			return nil, errors.New("el archivo no contiene certificados reconocibles (PEM o DER)")
		}
		out = cs
	}
	if len(out) == 0 {
		return nil, errors.New("el archivo no contiene certificados")
	}
	return out, nil
}

// FileInfo describe un archivo de llave o certificados.
type FileInfo struct {
	Kind      string // privateKey | certificate | bundle
	KeyType   string
	Encrypted bool
	Certs     []Summary
}

// Inspect detecta si el archivo es una llave privada o certificado(s) y lo resume.
func Inspect(path, pass string) (FileInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FileInfo{}, fmt.Errorf("no se puede leer el archivo: %w", err)
	}
	if certs, err := ParseCerts(data); err == nil {
		fi := FileInfo{Kind: "certificate"}
		if len(certs) > 1 {
			fi.Kind = "bundle"
		}
		for _, c := range certs {
			fi.Certs = append(fi.Certs, Summarize(c))
		}
		return fi, nil
	}
	ki, err := ParseKey(data, pass)
	if err != nil {
		if errors.Is(err, ErrPassphraseRequired) {
			return FileInfo{Kind: "privateKey", Encrypted: true, KeyType: encryptedKeyHint(data), Certs: []Summary{}}, nil
		}
		if errors.Is(err, ErrBadPassphrase) {
			return FileInfo{Kind: "privateKey", Encrypted: true, Certs: []Summary{}}, err
		}
		return FileInfo{}, err
	}
	return FileInfo{Kind: "privateKey", KeyType: ki.Type, Encrypted: ki.Encrypted, Certs: []Summary{}}, nil
}

// publicOf devuelve la llave pública de una privada.
func publicOf(k crypto.PrivateKey) crypto.PublicKey {
	if s, ok := k.(interface{ Public() crypto.PublicKey }); ok {
		return s.Public()
	}
	return nil
}

// KeyMatchesCert comprueba que la llave privada corresponde al certificado.
func KeyMatchesCert(k crypto.PrivateKey, c *x509.Certificate) bool {
	pub := publicOf(k)
	eq, ok := pub.(interface{ Equal(crypto.PublicKey) bool })
	return ok && eq.Equal(c.PublicKey)
}

// encryptedKeyHint deduce el tipo de una llave cifrada cuando el formato lo permite.
func encryptedKeyHint(data []byte) string {
	b, _ := pem.Decode(data)
	if b == nil {
		return ""
	}
	switch b.Type {
	case "RSA PRIVATE KEY":
		return "RSA"
	case "EC PRIVATE KEY":
		return "ECDSA"
	}
	return ""
}
