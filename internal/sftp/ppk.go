package sftp

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/argon2"
)

func putString(b []byte, s []byte) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}

func putMpint(b []byte, n *big.Int) []byte {
	if n.Sign() == 0 {
		return binary.BigEndian.AppendUint32(b, 0)
	}
	raw := n.Bytes()
	if raw[0]&0x80 != 0 {
		raw = append([]byte{0}, raw...)
	}
	return putString(b, raw)
}

func wrap64(s string) []string {
	var out []string
	for len(s) > 64 {
		out = append(out, s[:64])
		s = s[64:]
	}
	return append(out, s)
}

// MarshalPPK convierte una llave ed25519 o RSA al formato PuTTY PPK v3 (para PuTTY/WinSCP/FileZilla).
// Con frase de paso usa Argon2id + AES-256-CBC como PuTTYgen.
func MarshalPPK(priv crypto.PrivateKey, comment, passphrase string) (string, error) {
	var algo string
	var pubBlob, privBlob []byte
	switch k := priv.(type) {
	case ed25519.PrivateKey:
		algo = "ssh-ed25519"
		pubBlob = putString(putString(nil, []byte(algo)), k.Public().(ed25519.PublicKey))
		privBlob = putString(nil, k.Seed())
	case *rsa.PrivateKey:
		algo = "ssh-rsa"
		pubBlob = putMpint(putMpint(putString(nil, []byte(algo)), big.NewInt(int64(k.E))), k.N)
		k.Precompute()
		qinv := k.Precomputed.Qinv
		if qinv == nil {
			qinv = new(big.Int).ModInverse(k.Primes[1], k.Primes[0])
		}
		privBlob = putMpint(putMpint(putMpint(putMpint(nil, k.D), k.Primes[0]), k.Primes[1]), qinv)
	default:
		return "", errors.New("PPK: tipo de llave no soportado")
	}

	encName := "none"
	var macKey []byte
	var argonLines []string
	if passphrase != "" {
		encName = "aes256-cbc"
		const mem, passes, par = 8192, 13, 1
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return "", err
		}
		d := argon2.IDKey([]byte(passphrase), salt, passes, mem, par, 80)
		key, iv := d[:32], d[32:48]
		macKey = d[48:80]
		if pad := (16 - len(privBlob)%16) % 16; pad > 0 {
			p := make([]byte, pad)
			rand.Read(p)
			privBlob = append(privBlob, p...)
		}
		argonLines = []string{"Key-Derivation: Argon2id", fmt.Sprintf("Argon2-Memory: %d", mem), fmt.Sprintf("Argon2-Passes: %d", passes),
			fmt.Sprintf("Argon2-Parallelism: %d", par), "Argon2-Salt: " + hex.EncodeToString(salt)}
		blk, err := aes.NewCipher(key)
		if err != nil {
			return "", err
		}
		plain := privBlob
		enc := make([]byte, len(plain))
		cipher.NewCBCEncrypter(blk, iv).CryptBlocks(enc, plain)
		defer func() { _ = enc }()
		return buildPPK(algo, encName, comment, pubBlob, plain, enc, macKey, argonLines), nil
	}
	return buildPPK(algo, encName, comment, pubBlob, privBlob, privBlob, nil, nil), nil
}

func buildPPK(algo, enc, comment string, pubBlob, privPlain, privOut, macKey []byte, argon []string) string {
	var md []byte
	md = putString(md, []byte(algo))
	md = putString(md, []byte(enc))
	md = putString(md, []byte(comment))
	md = putString(md, pubBlob)
	md = putString(md, privPlain)
	m := hmac.New(sha256.New, macKey)
	m.Write(md)
	pubLines := wrap64(base64.StdEncoding.EncodeToString(pubBlob))
	privLines := wrap64(base64.StdEncoding.EncodeToString(privOut))
	var sb strings.Builder
	sb.WriteString("PuTTY-User-Key-File-3: " + algo + "\n")
	sb.WriteString("Encryption: " + enc + "\n")
	sb.WriteString("Comment: " + comment + "\n")
	sb.WriteString(fmt.Sprintf("Public-Lines: %d\n", len(pubLines)))
	sb.WriteString(strings.Join(pubLines, "\n") + "\n")
	for _, l := range argon {
		sb.WriteString(l + "\n")
	}
	sb.WriteString(fmt.Sprintf("Private-Lines: %d\n", len(privLines)))
	sb.WriteString(strings.Join(privLines, "\n") + "\n")
	sb.WriteString("Private-MAC: " + hex.EncodeToString(m.Sum(nil)) + "\n")
	return sb.String()
}
