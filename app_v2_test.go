package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func v2pki(t *testing.T) (dir string, req Pkcs12Request) {
	t.Helper()
	dir = t.TempDir()
	now := time.Now()
	gen := func() *ecdsa.PrivateKey {
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		return k
	}
	mk := func(tm, parent *x509.Certificate, pub *ecdsa.PublicKey, signer *ecdsa.PrivateKey) *x509.Certificate {
		der, err := x509.CreateCertificate(rand.Reader, tm, parent, pub, signer)
		if err != nil {
			t.Fatal(err)
		}
		c, _ := x509.ParseCertificate(der)
		return c
	}
	rk, ik, lk := gen(), gen(), gen()
	rt := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(1000 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	root := mk(rt, rt, &rk.PublicKey, rk)
	it := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Inter"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(900 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	inter := mk(it, root, &ik.PublicKey, rk)
	lt := &x509.Certificate{SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "host.test"}, DNSNames: []string{"host.test"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(800 * time.Hour)}
	leaf := mk(lt, inter, &lk.PublicKey, ik)
	pc := func(c *x509.Certificate) []byte {
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	}
	kd, _ := x509.MarshalPKCS8PrivateKey(lk)
	w := func(n string, b []byte) string {
		p := filepath.Join(dir, n)
		os.WriteFile(p, b, 0o600)
		return p
	}
	return dir, Pkcs12Request{PrivateKeyPath: w("k.pem", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kd})), CertPath: w("c.pem", pc(leaf)),
		IntermediatePath: w("i.pem", pc(inter)), RootPath: w("r.pem", pc(root)), Alias: "MiAlias", Password: "Cl4ve-Secreta-P12", OutputPath: filepath.Join(dir, "sal ida", "ñ.p12"), Format: "modern"}
}

// El .p12, su historial, las operaciones y los errores nunca contienen la contraseña.
func TestBuildPkcs12NeverLeaksPassword(t *testing.T) {
	a, _ := newTestApp(t)
	_, req := v2pki(t)
	res, err := a.BuildPkcs12(req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Verification.OK || res.Verification.ChainLength != 3 || res.Verification.Alias != "MiAlias" {
		t.Errorf("%+v", res.Verification)
	}
	hist, _ := a.ListCertificateHistory(10)
	if len(hist) != 1 || hist[0].Alias != "MiAlias" || hist[0].CommonName != "host.test" || !hist[0].Verified || hist[0].ChainLength != 3 {
		t.Errorf("historial: %+v", hist)
	}
	ops, _ := a.ListOperations(20)
	noSecret(t, "resultado", mustJSON(t, res), req.Password)
	noSecret(t, "historial", mustJSON(t, hist), req.Password)
	noSecret(t, "operaciones", mustJSON(t, ops), req.Password)

	// fallos: contraseña correcta usada en la petición no aparece en ningún mensaje
	bad := req
	bad.KeyPassphrase = "frase-privada-xyz"
	bad.PrivateKeyPath = req.CertPath // no es una llave
	_, err = a.BuildPkcs12(bad)
	if err == nil {
		t.Fatal("debía fallar")
	}
	noSecret(t, "error", err.Error(), req.Password, "frase-privada-xyz")
	if _, err := a.VerifyPkcs12(req.OutputPath, "otra-clave-mala", "MiAlias"); err == nil {
		t.Error("contraseña incorrecta debía fallar")
	} else {
		noSecret(t, "error verify", err.Error(), "otra-clave-mala", req.Password)
	}
	ops, _ = a.ListOperations(50)
	noSecret(t, "operaciones", mustJSON(t, ops), req.Password, "frase-privada-xyz", "otra-clave-mala")
	// el fallo se registra, y el historial no crece
	if h, _ := a.ListCertificateHistory(10); len(h) != 1 {
		t.Errorf("un build fallido no debe entrar al historial: %d", len(h))
	}
	// VerifyPkcs12 vía app con alias en otra capitalización
	v, err := a.VerifyPkcs12(req.OutputPath, req.Password, "MiAlias")
	if err != nil || !v.OK {
		t.Errorf("%v %+v", err, v)
	}
}

func TestInspectCertFileViaApp(t *testing.T) {
	a, _ := newTestApp(t)
	_, req := v2pki(t)
	info, err := a.InspectCertFile(req.CertPath, "")
	if err != nil || info.Kind != "certificate" || len(info.Certs) != 1 || info.Certs[0].CommonName != "host.test" || info.Certs[0].SANs == nil {
		t.Fatalf("%+v %v", info, err)
	}
	info, err = a.InspectCertFile(req.PrivateKeyPath, "")
	if err != nil || info.Kind != "privateKey" || info.Encrypted || !strings.Contains(info.KeyType, "ECDSA") || info.Certs == nil {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err := a.InspectCertFile(filepath.Join(t.TempDir(), "no"), ""); err == nil {
		t.Error("archivo inexistente debía fallar")
	}
	txt := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(txt, []byte("hola"), 0o600)
	if _, err := a.InspectCertFile(txt, ""); err == nil {
		t.Error("archivo de texto debía fallar")
	}
}

// Contrato v2: los tipos de respuesta no exponen secretos y usan camelCase.
func TestV2TypesContract(t *testing.T) {
	for _, v := range []any{SftpService{}, SftpKey{}, SftpUser{}, SftpUserResult{}, SftpLoginResult{}, CertSummary{}, CertFileInfo{},
		Pkcs12Verification{}, Pkcs12Check{}, Pkcs12Result{}, CertHistory{}} {
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "" || tag[0] < 'a' || tag[0] > 'z' || strings.ContainsAny(tag, "_-") {
				t.Errorf("%s.%s tag %q", rt.Name(), f.Name, tag)
			}
			low := strings.ToLower(f.Name)
			if (strings.Contains(low, "password") && f.Name != "HasPassword") || strings.Contains(low, "passphrase") || strings.Contains(low, "secret") {
				t.Errorf("%s.%s expone un campo sensible en una respuesta", rt.Name(), f.Name)
			}
		}
	}
}
