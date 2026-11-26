package certs

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/youmark/pkcs8"
)

type pki struct {
	rootKey, interKey, leafKey *ecdsa.PrivateKey
	root, inter, leaf          *x509.Certificate
}

func mk(t *testing.T, tmpl, parent *x509.Certificate, pub crypto.PublicKey, signer crypto.Signer) *x509.Certificate {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signer)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newPKI(t *testing.T, leafNotAfter time.Time) *pki {
	t.Helper()
	gen := func() *ecdsa.PrivateKey {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	p := &pki{rootKey: gen(), interKey: gen(), leafKey: gen()}
	now := time.Now()
	rootT := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test Root CA"}, NotBefore: now.Add(-48 * time.Hour),
		NotAfter: now.Add(3650 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	p.root = mk(t, rootT, rootT, &p.rootKey.PublicKey, p.rootKey)
	interT := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Test Intermediate CA"}, NotBefore: now.Add(-48 * time.Hour),
		NotAfter: now.Add(3000 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	p.inter = mk(t, interT, p.root, &p.interKey.PublicKey, p.rootKey)
	leafT := &x509.Certificate{SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "www.example.test"}, DNSNames: []string{"www.example.test", "example.test"},
		NotBefore: now.Add(-24 * time.Hour), NotAfter: leafNotAfter}
	p.leaf = mk(t, leafT, p.inter, &p.leafKey.PublicKey, p.interKey)
	return p
}

func pemCert(cs ...*x509.Certificate) []byte {
	var out []byte
	for _, c := range cs {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	return out
}

func write(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func keyPEM(t *testing.T, k *ecdsa.PrivateKey, kind string) []byte {
	t.Helper()
	switch kind {
	case "ec":
		der, _ := x509.MarshalECPrivateKey(k)
		return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	default:
		der, _ := x509.MarshalPKCS8PrivateKey(k)
		return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	}
}

type files struct{ key, leaf, inter, root, out string }

func setup(t *testing.T, p *pki) files {
	d := t.TempDir()
	return files{
		key: write(t, d, "key.pem", keyPEM(t, p.leafKey, "pkcs8")), leaf: write(t, d, "cert.pem", pemCert(p.leaf)),
		inter: write(t, d, "inter.pem", pemCert(p.inter)), root: write(t, d, "root.pem", pemCert(p.root)), out: filepath.Join(d, "out.p12"),
	}
}

func req(f files, root bool, format string) BuildRequest {
	r := BuildRequest{PrivateKeyPath: f.key, CertPath: f.leaf, IntermediatePath: f.inter, Alias: "tomcat", Password: "changeit", OutputPath: f.out, Format: format}
	if root {
		r.RootPath = f.root
	}
	return r
}

func TestBuildVerifyRoundTrip(t *testing.T) {
	p := newPKI(t, time.Now().Add(365*24*time.Hour))
	for _, format := range []string{"modern", "legacy"} {
		for _, withRoot := range []bool{true, false} {
			f := setup(t, p)
			res, err := Build(context.Background(), req(f, withRoot, format))
			if err != nil {
				t.Fatalf("%s root=%v: %v", format, withRoot, err)
			}
			want := 2
			if withRoot {
				want = 3
			}
			v := res.Verification
			if !v.OK || v.EntryType != "PrivateKeyEntry" || v.ChainLength != want || !v.KeyMatchesCert || !strings.EqualFold(v.Alias, "tomcat") {
				t.Fatalf("%s root=%v: verificación %+v", format, withRoot, v)
			}
			if v.KeytoolAvailable && !v.KeytoolOK {
				t.Errorf("%s root=%v: keytool no validó:\n%s", format, withRoot, v.KeytoolOutput)
			}
			t.Logf("%s root=%v keytool=%v", format, withRoot, v.KeytoolAvailable)
		}
	}
}

func TestAliasIsChecked(t *testing.T) {
	p := newPKI(t, time.Now().Add(365*24*time.Hour))
	f := setup(t, p)
	r := req(f, true, "modern")
	r.Alias = "miAlias"
	if _, err := Build(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	v, err := Verify(context.Background(), f.out, "changeit", "miAlias")
	if err != nil || !v.OK {
		t.Fatalf("%v %+v", err, v)
	}
	v, err = Verify(context.Background(), f.out, "changeit", "otroAlias")
	if err != nil {
		t.Fatal(err)
	}
	if v.OK {
		t.Fatal("un alias distinto debe fallar la verificación")
	}
	if _, err := Verify(context.Background(), f.out, "mala-clave", "miAlias"); err == nil {
		t.Fatal("contraseña incorrecta debe fallar")
	}
}

func TestDefaultAlias(t *testing.T) {
	p := newPKI(t, time.Now().Add(365*24*time.Hour))
	f := setup(t, p)
	r := req(f, false, "modern")
	r.Alias = ""
	res, err := Build(context.Background(), r)
	if err != nil || !strings.EqualFold(res.Verification.Alias, "tomcat") {
		t.Fatalf("%v %+v", err, res.Verification)
	}
}

func TestWrongKey(t *testing.T) {
	p := newPKI(t, time.Now().Add(365*24*time.Hour))
	f := setup(t, p)
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	f.key = write(t, filepath.Dir(f.key), "other.pem", keyPEM(t, other, "ec"))
	if _, err := Build(context.Background(), req(f, true, "modern")); err == nil || !strings.Contains(err.Error(), "NO corresponde") {
		t.Fatalf("esperaba error de llave: %v", err)
	}
	if _, err := os.Stat(f.out); err == nil {
		t.Fatal("no debe generarse el archivo")
	}
}

func TestOutOfOrderChainIsReordered(t *testing.T) {
	p := newPKI(t, time.Now().Add(365*24*time.Hour))
	f := setup(t, p)
	// raíz e intermedio en un solo archivo, en orden inverso, pasados como "intermedio"
	f.inter = write(t, filepath.Dir(f.key), "mix.pem", pemCert(p.root, p.inter))
	f.root = ""
	res, err := Build(context.Background(), req(f, false, "modern"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Verification.ChainLength != 3 || !res.Verification.OK {
		t.Fatalf("%+v", res.Verification)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "fuera de orden") {
			found = true
		}
	}
	if !found {
		t.Errorf("faltó el aviso de orden: %v", res.Warnings)
	}
}

func TestMissingIntermediate(t *testing.T) {
	p := newPKI(t, time.Now().Add(365*24*time.Hour))
	f := setup(t, p)
	f.inter = ""
	r := req(f, true, "modern")
	r.IntermediatePath = ""
	_, err := Build(context.Background(), r) // solo hoja + raíz: la raíz no firmó la hoja
	if err == nil || !strings.Contains(err.Error(), "intermedio") {
		t.Fatalf("esperaba error de intermedio faltante: %v", err)
	}
}

func TestRootMustBeSelfSigned(t *testing.T) {
	p := newPKI(t, time.Now().Add(365*24*time.Hour))
	f := setup(t, p)
	f.root = write(t, filepath.Dir(f.key), "fakeroot.pem", pemCert(p.inter)) // el intermedio no es raíz
	_, err := Build(context.Background(), req(f, true, "modern"))
	if err == nil || !strings.Contains(err.Error(), "autofirmado") {
		t.Fatalf("esperaba error de raíz no autofirmada: %v", err)
	}
}

func TestExpiredAndExpiringWarnings(t *testing.T) {
	p := newPKI(t, time.Now().Add(-1*time.Hour))
	f := setup(t, p)
	res, err := Build(context.Background(), req(f, false, "modern"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 || !strings.Contains(strings.Join(res.Warnings, " "), "VENCIDO") {
		t.Errorf("faltó aviso de vencido: %v", res.Warnings)
	}
	p2 := newPKI(t, time.Now().Add(10*24*time.Hour))
	res2, err := Build(context.Background(), req(setup(t, p2), false, "modern"))
	if err != nil || !strings.Contains(strings.Join(res2.Warnings, " "), "menos de 30 días") {
		t.Errorf("faltó aviso de próximo vencimiento: %v %v", err, res2.Warnings)
	}
}

func TestPasswordTooShortAndFormat(t *testing.T) {
	p := newPKI(t, time.Now().Add(365*24*time.Hour))
	f := setup(t, p)
	r := req(f, false, "modern")
	r.Password = "12345"
	if _, err := Build(context.Background(), r); err == nil {
		t.Fatal("contraseña corta debe fallar")
	}
	r.Password = "123456"
	r.Format = "raro"
	if _, err := Build(context.Background(), r); err == nil {
		t.Fatal("formato inválido debe fallar")
	}
}

func TestEncryptedKeys(t *testing.T) {
	p := newPKI(t, time.Now().Add(365*24*time.Hour))
	d := t.TempDir()
	// PKCS#8 cifrado
	der, _ := x509.MarshalPKCS8PrivateKey(p.leafKey)
	enc, err := pkcs8.MarshalPrivateKey(p.leafKey, []byte("secreto"), nil)
	_ = der
	if err != nil {
		t.Fatal(err)
	}
	k8 := write(t, d, "enc8.pem", pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: enc}))
	// PEM legado Proc-Type (RSA)
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	blk, err := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(rk), []byte("legado"), x509.PEMCipherAES256) //nolint:staticcheck
	if err != nil {
		t.Fatal(err)
	}
	kl := write(t, d, "legacy.pem", pem.EncodeToMemory(blk))

	if _, err := LoadKey(k8, ""); err != ErrPassphraseRequired {
		t.Errorf("sin frase: %v", err)
	}
	if _, err := LoadKey(k8, "mala"); err != ErrBadPassphrase {
		t.Errorf("frase mala: %v", err)
	}
	ki, err := LoadKey(k8, "secreto")
	if err != nil || !ki.Encrypted || !strings.HasPrefix(ki.Type, "ECDSA") || !KeyMatchesCert(ki.Key, p.leaf) {
		t.Fatalf("pkcs8 cifrado: %v %+v", err, ki)
	}
	if _, err := LoadKey(kl, ""); err != ErrPassphraseRequired {
		t.Errorf("legado sin frase: %v", err)
	}
	ki, err = LoadKey(kl, "legado")
	if err != nil || ki.Type != "RSA 2048" {
		t.Fatalf("legado: %v %+v", err, ki)
	}
	// build con llave cifrada
	f := setup(t, p)
	f.key = k8
	r := req(f, true, "modern")
	r.KeyPassphrase = "secreto"
	if res, err := Build(context.Background(), r); err != nil || !res.Verification.OK {
		t.Fatalf("build con llave cifrada: %v", err)
	}
}

func TestInspectAndDER(t *testing.T) {
	p := newPKI(t, time.Now().Add(365*24*time.Hour))
	d := t.TempDir()
	bundle := write(t, d, "b.pem", pemCert(p.leaf, p.inter, p.root))
	fi, err := Inspect(bundle, "")
	if err != nil || fi.Kind != "bundle" || len(fi.Certs) != 3 || fi.Certs[0].CommonName != "www.example.test" || len(fi.Certs[0].SANs) != 2 || !fi.Certs[2].SelfSigned || !fi.Certs[1].IsCA {
		t.Fatalf("%v %+v", err, fi)
	}
	der := write(t, d, "leaf.der", p.leaf.Raw)
	if fi, err := Inspect(der, ""); err != nil || fi.Kind != "certificate" {
		t.Fatalf("der: %v %+v", err, fi)
	}
	kder := func() []byte { b, _ := x509.MarshalECPrivateKey(p.leafKey); return b }()
	if fi, err := Inspect(write(t, d, "k.der", kder), ""); err != nil || fi.Kind != "privateKey" || fi.KeyType != "ECDSA P-256" {
		t.Fatalf("key der: %v %+v", err, fi)
	}
	if _, err := Inspect(write(t, d, "x.txt", []byte("hola")), ""); err == nil {
		t.Fatal("archivo basura debe fallar")
	}
}

func TestParseKeytool(t *testing.T) {
	k := ParseKeytool("Keystore type: PKCS12\nAlias name: tomcat\nCreation date: x\nEntry type: PrivateKeyEntry\nCertificate chain length: 3\n")
	if k.Alias != "tomcat" || k.EntryType != "PrivateKeyEntry" || k.ChainLength != 3 {
		t.Fatalf("%+v", k)
	}
}

func TestInspectEncryptedKeyWithoutPassphrase(t *testing.T) {
	p := newPKI(t, time.Now().Add(365*24*time.Hour))
	d := t.TempDir()
	enc, _ := pkcs8.MarshalPrivateKey(p.leafKey, []byte("secreto"), nil)
	f := write(t, d, "enc.key", pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: enc}))
	fi, err := Inspect(f, "")
	if err != nil || fi.Kind != "privateKey" || !fi.Encrypted {
		t.Fatalf("sin frase no debe fallar: %v %+v", err, fi)
	}
	if _, err := Inspect(f, "mala"); err == nil || err.Error() != "frase de paso incorrecta" {
		t.Fatalf("frase mala: %v", err)
	}
	if fi, err := Inspect(f, "secreto"); err != nil || !strings.HasPrefix(fi.KeyType, "ECDSA") {
		t.Fatalf("%v %+v", err, fi)
	}
	// RSA legado cifrado: el tipo se deduce del encabezado PEM
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	blk, _ := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(rk), []byte("x"), x509.PEMCipherAES128) //nolint:staticcheck
	lf := write(t, d, "legacy.key", pem.EncodeToMemory(blk))
	if fi, err := Inspect(lf, ""); err != nil || fi.KeyType != "RSA" || !fi.Encrypted {
		t.Fatalf("%v %+v", err, fi)
	}
}
