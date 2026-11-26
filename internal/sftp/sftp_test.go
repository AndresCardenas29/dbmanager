package sftp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbmanager/internal/dockerx"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ssh"
)

func TestUsersConfAndHook(t *testing.T) {
	us := []UserCfg{{Name: "alice", UID: 1001, UploadDir: "upload"}, {Name: "bob", UID: 1002, UploadDir: "in", PasswordHash: "$6$salt$hash"}}
	got := UsersConf(us)
	want := "alice::1001:100:upload\nbob:$6$salt$hash:e:1002:100:in\n"
	if got != want {
		t.Fatalf("users.conf:\n%q\nesperado\n%q", got, want)
	}
	h := HookScript(us)
	if strings.Contains(h, "\r") || !strings.HasPrefix(h, "#!/bin/sh\n") {
		t.Fatal("el script debe usar LF y shebang")
	}
	if !strings.Contains(h, `chown -R 1001:100 "/home/alice/upload"`) || !strings.Contains(h, "/etc/sftp-keys/ssh_host_*") {
		t.Fatalf("hook: %s", h)
	}
}

func TestValidation(t *testing.T) {
	for _, ok := range []string{"alice", "_svc", "a-b_c9", strings.Repeat("a", 32)} {
		if ValidUserName(ok) != nil {
			t.Errorf("%q debe ser válido", ok)
		}
	}
	for _, bad := range []string{"", "Alice", "1abc", "a b", "a:b", "root", strings.Repeat("a", 33), "ñ", "a;b"} {
		if ValidUserName(bad) == nil {
			t.Errorf("%q debe ser inválido", bad)
		}
	}
	if ValidUploadDir("upload") != nil || ValidUploadDir("a/b") == nil || ValidUploadDir("..") == nil || ValidUploadDir("") == nil {
		t.Error("uploadDir")
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("S3cret!")
	if err != nil || !strings.HasPrefix(h, "$6$") || strings.Contains(h, ":") {
		t.Fatalf("%v %q", err, h)
	}
	if !VerifyPassword("S3cret!", h) || VerifyPassword("otra", h) {
		t.Fatal("verificación")
	}
}

func TestGenerateAndParseKeys(t *testing.T) {
	for _, kt := range []string{KeyEd25519, KeyRSA4096} {
		if kt == KeyRSA4096 && testing.Short() {
			continue
		}
		g, err := Generate(kt, "", "u@x")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ssh.ParsePrivateKey(g.PrivatePEM); err != nil {
			t.Fatalf("%s: la privada generada no se puede leer: %v", kt, err)
		}
		canon, fp, typ, cmt, err := ParsePublicKey(g.PublicLine)
		if err != nil || fp != g.Fingerprint || canon != g.PublicLine || cmt != "u@x" || typ != g.Type {
			t.Fatalf("%s: %v %s %s %s %s", kt, err, canon, fp, typ, cmt)
		}
		// con frase de paso
		gp, err := Generate(kt, "frase", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ssh.ParsePrivateKey(gp.PrivatePEM); err == nil {
			t.Fatal("debe pedir frase de paso")
		}
		if _, err := ssh.ParsePrivateKeyWithPassphrase(gp.PrivatePEM, []byte("frase")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Generate("dsa", "", ""); err == nil {
		t.Fatal("tipo inválido")
	}
	for _, bad := range []string{"", "no es una llave", "command=\"ls\" ssh-ed25519 AAAA", "ssh-ed25519 AAAA\nssh-ed25519 BBBB"} {
		if _, _, _, _, err := ParsePublicKey(bad); err == nil {
			t.Errorf("%q debe rechazarse", bad)
		}
	}
}

// parsePPK lee un PPK v3 y valida el MAC (y descifra si hay frase). Devuelve los blobs.
func parsePPK(t *testing.T, s, pass string) (algo string, pubBlob, privBlob []byte) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(s), "\n")
	hdr := map[string]string{}
	var pub, priv string
	for i := 0; i < len(lines); i++ {
		k, v, _ := strings.Cut(lines[i], ": ")
		switch k {
		case "Public-Lines", "Private-Lines":
			n := 0
			for _, c := range v {
				n = n*10 + int(c-'0')
			}
			joined := strings.Join(lines[i+1:i+1+n], "")
			if k == "Public-Lines" {
				pub = joined
			} else {
				priv = joined
			}
			i += n
		default:
			hdr[k] = v
		}
	}
	algo = hdr["PuTTY-User-Key-File-3"]
	pubBlob, _ = base64.StdEncoding.DecodeString(pub)
	privBlob, _ = base64.StdEncoding.DecodeString(priv)
	var macKey []byte
	if hdr["Encryption"] == "aes256-cbc" {
		salt, _ := hex.DecodeString(hdr["Argon2-Salt"])
		d := argon2.IDKey([]byte(pass), salt, 13, 8192, 1, 80)
		macKey = d[48:80]
		blk, _ := aes.NewCipher(d[:32])
		plain := make([]byte, len(privBlob))
		cipher.NewCBCDecrypter(blk, d[32:48]).CryptBlocks(plain, privBlob)
		privBlob = plain
	}
	str := func(b []byte, s string) []byte {
		b = binary.BigEndian.AppendUint32(b, uint32(len(s)))
		return append(b, s...)
	}
	var md []byte
	md = str(md, algo)
	md = str(md, hdr["Encryption"])
	md = str(md, hdr["Comment"])
	md = str(md, string(pubBlob))
	md = str(md, string(privBlob))
	m := hmac.New(sha256.New, macKey)
	m.Write(md)
	if hex.EncodeToString(m.Sum(nil)) != hdr["Private-MAC"] {
		t.Fatal("el MAC del PPK no coincide")
	}
	return
}

func TestPPKFormat(t *testing.T) {
	g, err := Generate(KeyEd25519, "", "c")
	if err != nil {
		t.Fatal(err)
	}
	for _, pass := range []string{"", "pp"} {
		s, err := MarshalPPK(g.Private, "c", pass)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(s, "PuTTY-User-Key-File-3: ssh-ed25519\n") || strings.Contains(s, "\r") {
			t.Fatalf("cabecera: %s", s)
		}
		algo, pub, priv := parsePPK(t, s, pass)
		if algo != "ssh-ed25519" {
			t.Fatal(algo)
		}
		// la parte pública del PPK debe ser la llave pública SSH (wire format)
		sp, _ := ssh.ParsePublicKey(pub)
		if sp == nil || ssh.FingerprintSHA256(sp) != g.Fingerprint {
			t.Fatal("la parte pública del PPK no coincide")
		}
		// el privado contiene la semilla de 32 bytes
		if len(priv) < 36 || binary.BigEndian.Uint32(priv[:4]) != 32 {
			t.Fatalf("blob privado: %x", priv)
		}
	}
	if !testing.Short() {
		r, err := Generate(KeyRSA4096, "", "r")
		if err != nil {
			t.Fatal(err)
		}
		s, err := MarshalPPK(r.Private, "r", "")
		if err != nil {
			t.Fatal(err)
		}
		_, pub, _ := parsePPK(t, s, "")
		sp, _ := ssh.ParsePublicKey(pub)
		if sp == nil || ssh.FingerprintSHA256(sp) != r.Fingerprint {
			t.Fatal("RSA: parte pública")
		}
	}
}

func TestWriteConfigAndHostKeys(t *testing.T) {
	base := t.TempDir()
	us := []UserCfg{{Name: "alice", UID: 1001, UploadDir: "upload", PublicKeys: []string{"ssh-ed25519 AAAA a", "ssh-ed25519 BBBB b"}}, {Name: "bob", UID: 1002, UploadDir: "upload", PasswordHash: "$6$x$y"}}
	p, err := WriteConfig(base, us)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p.UsersConf)
	if string(b) != UsersConf(us) {
		t.Fatal("users.conf")
	}
	for _, f := range []string{"alice/key1.pub", "alice/key2.pub"} {
		if _, err := os.Stat(filepath.Join(p.KeysDir, f)); err != nil {
			t.Fatal(f, err)
		}
	}
	if st, err := os.Stat(filepath.Join(p.KeysDir, "bob")); err != nil || !st.IsDir() {
		t.Fatal("bob debe tener su carpeta de llaves (se monta en el contenedor)")
	}
	fp1, err := HostFingerprints(p.HostKeys)
	if err != nil || len(fp1) != 2 || !strings.HasPrefix(fp1[0], "ED25519 SHA256:") {
		t.Fatalf("%v %v", err, fp1)
	}
	// regenerar la configuración NO cambia las llaves de host
	us[0].PublicKeys = nil
	if _, err := WriteConfig(base, us); err != nil {
		t.Fatal(err)
	}
	fp2, _ := HostFingerprints(p.HostKeys)
	if strings.Join(fp1, ",") != strings.Join(fp2, ",") {
		t.Fatal("las llaves de host cambiaron al regenerar la configuración")
	}
	if _, err := os.Stat(filepath.Join(p.KeysDir, "alice", "key1.pub")); err == nil {
		t.Fatal("las llaves quitadas deben desaparecer")
	}
	if st, _ := os.Stat(filepath.Join(p.HostKeys, "ssh_host_ed25519_key")); st != nil && os.PathSeparator == '/' && st.Mode().Perm() != 0o600 {
		t.Errorf("permisos de la llave de host: %v", st.Mode())
	}
}

func TestIsSftpContainer(t *testing.T) {
	cases := []struct {
		name string
		info dockerx.ContainerInfo
		want bool
	}{
		{"imagen", dockerx.ContainerInfo{Image: "atmoz/sftp:alpine@sha256:abc"}, true},
		{"env", dockerx.ContainerInfo{Image: "sftp-sftp:latest", EnvNames: []string{"SFTP_USERS"}}, true},
		{"montaje", dockerx.ContainerInfo{Image: "x", Mounts: []dockerx.MountInfo{{Destination: "/etc/sftp/users.conf"}}}, true},
		{"entrypoint+22", dockerx.ContainerInfo{Image: "x", Entrypoint: []string{"/entrypoint"}, Ports: []dockerx.PortMapping{{ContainerPort: 22, HostPort: 2223}}}, true},
		{"entrypoint sin 22", dockerx.ContainerInfo{Image: "x", Entrypoint: []string{"/entrypoint"}}, false},
		{"postgres", dockerx.ContainerInfo{Image: "postgres:17", EnvNames: []string{"POSTGRES_PASSWORD"}}, false},
	}
	for _, c := range cases {
		if got := IsSftpContainer(c.info); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
