//go:build integration

package sftp

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// Valida los .ppk generados con puttygen REAL (paquete putty-tools de Debian) en un contenedor desechable
// dbmanager-test-puttygen (--rm). Requiere red para apt-get.
func TestPPKValidatedByPuttygen(t *testing.T) {
	dir := t.TempDir()
	type kc struct{ name, keyType, pass string }
	cases := []kc{
		{"ed_plain", KeyEd25519, ""}, {"ed_pass", KeyEd25519, "frase de paso ñ 1!"},
		{"rsa_plain", KeyRSA4096, ""}, {"rsa_pass", KeyRSA4096, "otra-frase-$x'\""},
	}
	pubs := map[string]string{}
	for _, c := range cases {
		g, err := Generate(c.keyType, c.pass, "comentario con espacios")
		if err != nil {
			t.Fatal(err)
		}
		ppk, err := MarshalPPK(g.Private, "comentario con espacios", c.pass)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, c.name+".ppk"), []byte(ppk), 0o600)
		os.WriteFile(filepath.Join(dir, c.name+".pass"), []byte(c.pass), 0o600)
		os.WriteFile(filepath.Join(dir, c.name+".wrong"), []byte("incorrecta"), 0o600)
		pubs[c.name] = g.PublicLine
	}
	script := `set -u
apt-get update -qq >/dev/null 2>&1 && apt-get install -y -qq putty-tools >/dev/null 2>&1 || { echo NOAPT > /k/status; exit 0; }
cd /k
for n in ed_plain ed_pass rsa_plain rsa_pass; do
  puttygen $n.ppk -O public-openssh -o $n.pub.out 2> $n.pub.err; echo $? > $n.pub.rc
  if [ -s $n.pass ]; then pf="--old-passphrase $n.pass"; else pf=""; fi
  puttygen $n.ppk $pf -O private-openssh-new -o $n.priv.out 2> $n.priv.err; echo $? > $n.priv.rc
  puttygen $n.ppk --old-passphrase $n.wrong -O private-openssh-new -o $n.wrong.out 2> $n.wrong.err; echo $? > $n.wrong.rc
  puttygen $n.ppk $pf -l > $n.fp 2>&1
done
echo OK > /k/status
`
	name := "dbmanager-test-puttygen"
	exec.Command("docker", "rm", "-f", name).Run() // por si quedó uno viejo (solo el nuestro)
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	cmd := exec.Command("docker", "run", "--rm", "--name", name, "-v", dir+":/k", "debian:stable-slim", "sh", "-c", script)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("docker run: %v\n%s", err, out.String())
		}
	case <-time.After(8 * time.Minute):
		exec.Command("docker", "rm", "-f", name).Run()
		t.Fatal("tiempo agotado")
	}
	st, _ := os.ReadFile(filepath.Join(dir, "status"))
	if strings.TrimSpace(string(st)) != "OK" {
		t.Skipf("no se pudo instalar putty-tools (¿sin red?): %s %s", st, out.String())
	}
	rc := func(f string) string { b, _ := os.ReadFile(filepath.Join(dir, f)); return strings.TrimSpace(string(b)) }
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if rc(c.name+".pub.rc") != "0" {
				e, _ := os.ReadFile(filepath.Join(dir, c.name+".pub.err"))
				t.Fatalf("puttygen no leyó el .ppk: %s", e)
			}
			got, _ := os.ReadFile(filepath.Join(dir, c.name+".pub.out"))
			want := strings.Fields(pubs[c.name])
			gf := strings.Fields(string(got))
			if len(gf) < 2 || gf[0] != want[0] || gf[1] != want[1] {
				t.Errorf("la llave pública de puttygen difiere del .pub\n got: %s\nwant: %s", got, pubs[c.name])
			}
			// la privada (descifrada y verificada con MAC por puttygen) debe corresponder a la misma pública
			if rc(c.name+".priv.rc") != "0" {
				e, _ := os.ReadFile(filepath.Join(dir, c.name+".priv.err"))
				t.Fatalf("puttygen no pudo descifrar/verificar la parte privada (MAC): %s", e)
			}
			pb, _ := os.ReadFile(filepath.Join(dir, c.name+".priv.out"))
			var signer ssh.Signer
			var err error
			if c.pass != "" { // puttygen conserva la frase al convertir
				signer, err = ssh.ParsePrivateKeyWithPassphrase(pb, []byte(c.pass))
			} else {
				signer, err = ssh.ParsePrivateKey(pb)
			}
			if err != nil {
				t.Fatalf("la privada exportada por puttygen no se puede leer: %v", err)
			}
			if got := strings.Fields(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))); got[1] != want[1] {
				t.Errorf("la privada contenida en el .ppk no corresponde a la pública")
			}
			// frase incorrecta debe fallar con passphrase; sin passphrase puttygen ignora la frase
			wr := rc(c.name + ".wrong.rc")
			if c.pass != "" && wr == "0" {
				t.Errorf("puttygen aceptó una frase incorrecta")
			}
			fp, _ := os.ReadFile(filepath.Join(dir, c.name+".fp"))
			t.Logf("%s: puttygen -l -> %s", c.name, strings.TrimSpace(string(fp)))
			_ = fmt.Sprint()
		})
	}
}
