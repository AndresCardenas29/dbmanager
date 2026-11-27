package certs

// Validación cruzada con herramientas EXTERNAS reales (openssl, keytool). Se omite lo que no esté instalado.
// Prefijo "x" en los helpers para no chocar con los de certs_test.go.

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const xpw = "Sup3r-Secr3t-p12!"

func findOpenSSL(t testing.TB) string {
	t.Helper()
	if p, err := exec.LookPath("openssl"); err == nil {
		return p
	}
	for _, p := range []string{`C:\Program Files\OpenSSL-Win64\bin\openssl.exe`, `C:\Program Files\Git\mingw64\bin\openssl.exe`, "/usr/bin/openssl", "/opt/homebrew/bin/openssl"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("openssl no está instalado")
	return ""
}

func needKeytool(t testing.TB) string {
	t.Helper()
	kt := FindKeytool()
	if kt == "" {
		t.Skip("keytool no está instalado")
	}
	return kt
}

func xrun(t testing.TB, env []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	return b.String(), err
}

// xchain es una PKI de prueba con n intermedios.
type xchain struct {
	kind     string
	keys     []crypto.Signer // [0]=raíz, 1..n=intermedios, último=hoja
	certs    []*x509.Certificate
	leafKey  crypto.Signer
	nInter   int
	notAfter time.Time
}

func xgen(t testing.TB, kind string) crypto.Signer {
	t.Helper()
	switch kind {
	case "rsa":
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		return k
	case "ec":
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	case "ed25519":
		_, k, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	t.Fatal("kind")
	return nil
}

func xcert(t testing.TB, tmpl, parent *x509.Certificate, pub crypto.PublicKey, signer crypto.Signer) *x509.Certificate {
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

func newXChain(t testing.TB, kind string, nInter int) *xchain {
	t.Helper()
	now := time.Now()
	x := &xchain{kind: kind, nInter: nInter}
	rk := xgen(t, kind)
	rootT := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "X Root CA"}, NotBefore: now.Add(-48 * time.Hour),
		NotAfter: now.Add(3650 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	root := xcert(t, rootT, rootT, rk.Public(), rk)
	x.keys, x.certs = []crypto.Signer{rk}, []*x509.Certificate{root}
	parent, pk := root, rk
	for i := 1; i <= nInter; i++ {
		k := xgen(t, kind)
		tm := &x509.Certificate{SerialNumber: big.NewInt(int64(10 + i)), Subject: pkix.Name{CommonName: fmt.Sprintf("X Intermediate %d", i)},
			NotBefore: now.Add(-48 * time.Hour), NotAfter: now.Add(3000 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
		c := xcert(t, tm, parent, k.Public(), pk)
		x.keys, x.certs = append(x.keys, k), append(x.certs, c)
		parent, pk = c, k
	}
	lk := xgen(t, kind)
	lt := &x509.Certificate{SerialNumber: big.NewInt(99), Subject: pkix.Name{CommonName: "leaf.example.test", Organization: []string{"Ñandú & Cía"}},
		DNSNames: []string{"leaf.example.test", "*.example.test"}, NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(365 * 24 * time.Hour)}
	x.leafKey = lk
	x.certs = append(x.certs, xcert(t, lt, parent, lk.Public(), pk))
	return x
}

func (x *xchain) root() *x509.Certificate     { return x.certs[0] }
func (x *xchain) leaf() *x509.Certificate     { return x.certs[len(x.certs)-1] }
func (x *xchain) inters() []*x509.Certificate { return x.certs[1 : len(x.certs)-1] }

func xpemCerts(cs ...*x509.Certificate) []byte {
	var out []byte
	for _, c := range cs {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	return out
}

func (x *xchain) keyPEM(t testing.TB, style string) []byte {
	t.Helper()
	switch style {
	case "pkcs8":
		der, err := x509.MarshalPKCS8PrivateKey(x.leafKey)
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	case "pkcs8der":
		der, _ := x509.MarshalPKCS8PrivateKey(x.leafKey)
		return der
	case "traditional":
		switch k := x.leafKey.(type) {
		case *rsa.PrivateKey:
			return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
		case *ecdsa.PrivateKey:
			der, _ := x509.MarshalECPrivateKey(k)
			return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
		}
	}
	t.Fatalf("estilo %s no aplica a %s", style, x.kind)
	return nil
}

func xwrite(t testing.TB, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

type xfiles struct{ dir, key, leaf, inter, root, out string }

func (x *xchain) files(t testing.TB) xfiles {
	d := t.TempDir()
	f := xfiles{dir: d, key: xwrite(t, d, "key.pem", x.keyPEM(t, "pkcs8")), leaf: xwrite(t, d, "cert.pem", xpemCerts(x.leaf())), out: filepath.Join(d, "out.p12")}
	if len(x.inters()) > 0 {
		f.inter = xwrite(t, d, "inter.pem", xpemCerts(x.inters()...))
	}
	f.root = xwrite(t, d, "root.pem", xpemCerts(x.root()))
	return f
}

func (f xfiles) req(alias, format string, withRoot bool) BuildRequest {
	r := BuildRequest{PrivateKeyPath: f.key, CertPath: f.leaf, IntermediatePath: f.inter, Alias: alias, Password: xpw, OutputPath: f.out, Format: format, Overwrite: true}
	if withRoot {
		r.RootPath = f.root
	}
	return r
}

// ---------------------------------------------------------------- app -> openssl + keytool

func TestXVal_AppBuildVerifiedByOpenSSLAndKeytool(t *testing.T) {
	ossl := findOpenSSL(t)
	kt := needKeytool(t)
	t.Logf("openssl=%s keytool=%s", ossl, kt)
	ctx := context.Background()
	for _, kind := range []string{"rsa", "ec"} {
		for _, format := range []string{"modern", "legacy"} {
			for _, withRoot := range []bool{false, true} {
				name := fmt.Sprintf("%s/%s/root=%v", kind, format, withRoot)
				t.Run(name, func(t *testing.T) {
					x := newXChain(t, kind, 1)
					f := x.files(t)
					res, err := Build(ctx, f.req("mialias", format, withRoot))
					if err != nil {
						t.Fatalf("Build: %v", err)
					}
					wantLen := 2
					if withRoot {
						wantLen = 3
					}
					if res.Verification.ChainLength != wantLen || !res.Verification.OK {
						t.Fatalf("verificación propia: %+v", res.Verification)
					}
					env := []string{"XPW=" + xpw}
					// --- keytool
					out, err := xrun(t, append(env, "JAVA_TOOL_OPTIONS="), kt, "-J-Duser.language=en", "-list", "-v", "-keystore", f.out, "-storetype", "PKCS12", "-storepass:env", "XPW")
					if err != nil {
						if kind == "ed25519" {
							t.Logf("keytool no abre p12 con llave ed25519: %v\n%s", err, out)
							return
						}
						t.Fatalf("keytool falló: %v\n%s", err, out)
					}
					ki := ParseKeytool(out)
					if ki.EntryType != "PrivateKeyEntry" || ki.Alias != "mialias" || ki.ChainLength != wantLen {
						t.Errorf("keytool: %+v\n%s", ki, out)
					}
					if strings.Contains(out, xpw) {
						t.Error("la salida de keytool contiene la contraseña")
					}
					// --- openssl: certificados y llave
					ocerts, err := xrun(t, env, ossl, "pkcs12", "-in", f.out, "-nokeys", "-passin", "env:XPW")
					if err != nil && format == "legacy" {
						t.Logf("openssl sin -legacy falló (%v); se reintenta con -legacy", err)
						ocerts, err = xrun(t, env, ossl, "pkcs12", "-legacy", "-in", f.out, "-nokeys", "-passin", "env:XPW")
					}
					if err != nil {
						t.Fatalf("openssl: %v\n%s", err, ocerts)
					}
					if n := strings.Count(ocerts, "BEGIN CERTIFICATE"); n != wantLen {
						t.Errorf("openssl ve %d certificados, esperaba %d\n%s", n, wantLen, ocerts)
					}
					info, err := xrun(t, env, ossl, "pkcs12", "-info", "-in", f.out, "-nodes", "-passin", "env:XPW")
					if err != nil && format == "legacy" {
						info, err = xrun(t, env, ossl, "pkcs12", "-legacy", "-info", "-in", f.out, "-nodes", "-passin", "env:XPW")
					}
					if err != nil {
						t.Fatalf("openssl -info: %v\n%s", err, info)
					}
					if !strings.Contains(info, "friendlyName: mialias") {
						t.Errorf("openssl no muestra friendlyName mialias:\n%s", info)
					}
					if !strings.Contains(info, "PRIVATE KEY") {
						t.Errorf("openssl no extrajo la llave privada")
					}
					if format == "modern" && !strings.Contains(strings.ToLower(info), "aes-256") {
						t.Errorf("formato modern debería usar AES-256:\n%s", firstLines(info, 8))
					}
					if format == "legacy" && !strings.Contains(strings.ToLower(info), "des") {
						t.Errorf("formato legacy debería usar 3DES/RC2:\n%s", firstLines(info, 8))
					}
					// la llave que extrae openssl coincide con el certificado hoja (módulo / pub)
					pubFromKey, err := xrunStdin(t, env, ossl, info, "pkey", "-pubout")
					if err != nil {
						t.Fatalf("openssl pkey: %v %s", err, pubFromKey)
					}
					der, _ := x509.MarshalPKIXPublicKey(x.leaf().PublicKey)
					wantPub := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
					if normPEM(pubFromKey) != normPEM(wantPub) {
						t.Errorf("la llave pública derivada por openssl no coincide con el certificado")
					}
				})
			}
		}
	}
}

func firstLines(s string, n int) string {
	l := strings.Split(s, "\n")
	if len(l) > n {
		l = l[:n]
	}
	return strings.Join(l, "\n")
}

func normPEM(s string) string { return strings.Join(strings.Fields(s), "") }

func xrunStdin(t testing.TB, env []string, name, stdin string, args ...string) (string, error) {
	// extrae el primer bloque PRIVATE KEY de stdin y lo pasa a openssl
	i := strings.Index(stdin, "-----BEGIN ")
	var blk string
	for rest := stdin[i:]; ; {
		b, r := pem.Decode([]byte(rest))
		if b == nil {
			break
		}
		if strings.HasSuffix(b.Type, "PRIVATE KEY") {
			blk = string(pem.EncodeToMemory(b))
			break
		}
		rest = string(r)
	}
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(blk)
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	return b.String(), err
}

// Cadena de 2 intermedios -> keytool debe ver 4 (o 3 sin raíz).
func TestXVal_TwoIntermediates(t *testing.T) {
	kt := needKeytool(t)
	x := newXChain(t, "rsa", 2)
	f := x.files(t)
	for _, withRoot := range []bool{false, true} {
		res, err := Build(context.Background(), f.req("tomcat", "modern", withRoot))
		if err != nil {
			t.Fatalf("root=%v: %v", withRoot, err)
		}
		want := 3
		if withRoot {
			want = 4
		}
		out, _ := xrun(t, []string{"XPW=" + xpw}, kt, "-list", "-v", "-keystore", f.out, "-storetype", "PKCS12", "-storepass:env", "XPW")
		if ki := ParseKeytool(out); ki.ChainLength != want || res.Verification.ChainLength != want {
			t.Errorf("root=%v: keytool=%+v app=%d esperado %d", withRoot, ki, res.Verification.ChainLength, want)
		}
	}
}

// ---------------------------------------------------------------- alias y mayúsculas

func TestXVal_KeytoolAliasCase(t *testing.T) {
	kt := needKeytool(t)
	ossl := findOpenSSL(t)
	x := newXChain(t, "rsa", 1)
	f := x.files(t)
	for _, alias := range []string{"MiAlias", "ALL CAPS", "con espacio"} {
		res, err := Build(context.Background(), f.req(alias, "modern", true))
		if err != nil {
			t.Errorf("alias %q: %v", alias, err)
			continue
		}
		out, kerr := xrun(t, []string{"XPW=" + xpw}, kt, "-J-Duser.language=en", "-list", "-v", "-keystore", f.out, "-storetype", "PKCS12", "-storepass:env", "XPW")
		ki := ParseKeytool(out)
		t.Logf("alias pedido %q -> keytool 25 imprime %q (err=%v); app.Verify alias=%q ok=%v", alias, ki.Alias, kerr, res.Verification.Alias, res.Verification.OK)
		inf, _ := xrun(t, []string{"XPW=" + xpw}, ossl, "pkcs12", "-info", "-in", f.out, "-nokeys", "-passin", "env:XPW")
		for _, l := range strings.Split(inf, "\n") {
			if strings.Contains(l, "friendlyName") {
				t.Logf("   openssl: %s", strings.TrimSpace(l))
				break
			}
		}
		if !res.Verification.OK {
			t.Errorf("alias %q: la verificación propia falló: %+v", alias, res.Verification.Checks)
		}
	}
}

// ---------------------------------------------------------------- openssl -> app (VerifyPkcs12)

func TestXVal_OpenSSLCreatedP12VerifiedByApp(t *testing.T) {
	ossl := findOpenSSL(t)
	kt := FindKeytool()
	for _, kind := range []string{"rsa", "ec"} {
		for _, legacy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/legacy=%v", kind, legacy), func(t *testing.T) {
				x := newXChain(t, kind, 1)
				f := x.files(t)
				chain := xwrite(t, f.dir, "chain.pem", xpemCerts(x.inters()[0], x.root()))
				out := filepath.Join(f.dir, "ossl.p12")
				args := []string{"pkcs12", "-export", "-inkey", f.key, "-in", f.leaf, "-certfile", chain, "-name", "miAlias", "-out", out, "-passout", "env:XPW"}
				if legacy {
					args = append(args, "-legacy")
				}
				if o, err := xrun(t, []string{"XPW=" + xpw}, ossl, args...); err != nil {
					if legacy && strings.Contains(o, "legacy") {
						t.Skipf("este openssl no tiene el proveedor legacy: %s", firstLines(o, 1))
					}
					t.Fatalf("openssl export: %v\n%s", err, o)
				}
				v, err := Verify(context.Background(), out, xpw, "miAlias")
				if err != nil {
					t.Fatalf("Verify: %v", err)
				}
				if !v.OK || v.Alias != "miAlias" || v.ChainLength != 3 || v.EntryType != "PrivateKeyEntry" || !v.KeyMatchesCert {
					t.Errorf("Verify de p12 de openssl: %+v", v)
				}
				// alias con otra capitalización también acepta (diseño documentado)
				v2, _ := Verify(context.Background(), out, xpw, "MIALIAS")
				t.Logf("alias esperado 'MIALIAS' sobre p12 con 'miAlias' -> OK=%v (checks: %+v)", v2.OK, v2.Checks)
				// alias distinto -> falla
				v3, _ := Verify(context.Background(), out, xpw, "otro")
				if v3.OK {
					t.Error("un alias distinto debía fallar la verificación")
				}
				if kt != "" {
					ko, _ := xrun(t, []string{"XPW=" + xpw}, kt, "-list", "-v", "-keystore", out, "-storetype", "PKCS12", "-storepass:env", "XPW")
					t.Logf("keytool sobre el p12 de openssl (alias 'miAlias') imprime alias %q", ParseKeytool(ko).Alias)
				}
				// contraseña incorrecta
				_, err = Verify(context.Background(), out, "mala-"+xpw, "miAlias")
				if err == nil || strings.Contains(err.Error(), xpw) {
					t.Errorf("contraseña incorrecta: %v", err)
				}
			})
		}
	}
	// openssl sin -name: no hay friendlyName; keytool usa el localKeyID como alias
	x := newXChain(t, "rsa", 1)
	f := x.files(t)
	out := filepath.Join(f.dir, "noname.p12")
	if o, err := xrun(t, []string{"XPW=" + xpw}, ossl, "pkcs12", "-export", "-inkey", f.key, "-in", f.leaf, "-certfile", f.inter, "-out", out, "-passout", "env:XPW"); err != nil {
		t.Fatalf("%v %s", err, o)
	}
	v, err := Verify(context.Background(), out, xpw, "")
	if err != nil || !v.OK {
		t.Errorf("p12 sin nombre: %v %+v", err, v)
	}
	t.Logf("p12 de openssl sin -name: alias según la app=%q", v.Alias)
}

// ---------------------------------------------------------------- negativos

func TestXVal_Negative(t *testing.T) {
	ossl := findOpenSSL(t)
	ctx := context.Background()
	t.Run("clave equivocada", func(t *testing.T) {
		x, y := newXChain(t, "rsa", 1), newXChain(t, "rsa", 1)
		f := x.files(t)
		xwrite(t, f.dir, "key.pem", y.keyPEM(t, "pkcs8"))
		_, err := Build(ctx, f.req("a", "modern", true))
		if err == nil || !strings.Contains(err.Error(), "NO corresponde") {
			t.Fatalf("%v", err)
		}
		if _, statErr := os.Stat(f.out); statErr == nil {
			t.Error("no debe crearse el .p12")
		}
	})
	t.Run("contraseña corta y que no aparece en errores", func(t *testing.T) {
		x := newXChain(t, "rsa", 1)
		f := x.files(t)
		r := f.req("a", "modern", true)
		r.Password = "abc"
		if _, err := Build(ctx, r); err == nil || strings.Contains(err.Error(), "abc'") {
			t.Fatal(err)
		}
	})
	t.Run("cadena desordenada en el archivo intermedio", func(t *testing.T) {
		x := newXChain(t, "rsa", 2)
		f := x.files(t)
		// raíz antes que los intermedios, y los intermedios invertidos
		xwrite(t, f.dir, "inter.pem", xpemCerts(x.root(), x.inters()[0], x.inters()[1]))
		res, err := Build(ctx, f.req("a", "modern", false))
		if err != nil {
			t.Fatal(err)
		}
		if res.Verification.ChainLength != 4 {
			// raíz incluida porque estaba en el archivo intermedio: documentamos
			t.Logf("longitud=%d", res.Verification.ChainLength)
		}
		if !res.Verification.OK {
			t.Errorf("%+v", res.Verification.Checks)
		}
		if !anyContains(res.Warnings, "fuera de orden") {
			t.Errorf("faltó el aviso de orden: %v", res.Warnings)
		}
	})
	t.Run("cadena desordenada dentro del certificado hoja (sin aviso)", func(t *testing.T) {
		x := newXChain(t, "rsa", 1)
		f := x.files(t)
		xwrite(t, f.dir, "cert.pem", xpemCerts(x.root(), x.inters()[0], x.leaf()))
		res, err := Build(ctx, f.req("a", "modern", false))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("chain=%d warnings=%v", res.Verification.ChainLength, res.Warnings)
		if res.Verification.ChainLength != 3 {
			t.Errorf("longitud %d", res.Verification.ChainLength)
		}
	})
	t.Run("falta el intermedio", func(t *testing.T) {
		x := newXChain(t, "rsa", 1)
		f := x.files(t)
		for _, withRoot := range []bool{false, true} {
			r := f.req("a", "modern", withRoot)
			r.IntermediatePath = ""
			if _, err := Build(ctx, r); err == nil || !strings.Contains(err.Error(), "falta el certificado intermedio") {
				t.Errorf("root=%v: %v", withRoot, err)
			}
		}
	})
	t.Run("archivo intermedio con 2 certificados (inter+raíz)", func(t *testing.T) {
		x := newXChain(t, "ec", 1)
		f := x.files(t)
		xwrite(t, f.dir, "inter.pem", xpemCerts(x.inters()[0], x.root()))
		res, err := Build(ctx, f.req("a", "modern", false))
		if err != nil {
			t.Fatal(err)
		}
		if res.Verification.ChainLength != 3 || len(res.Warnings) != 0 {
			t.Errorf("len=%d warnings=%v", res.Verification.ChainLength, res.Warnings)
		}
	})
	t.Run("DER (llave PKCS#8 y certificados)", func(t *testing.T) {
		x := newXChain(t, "rsa", 1)
		f := x.files(t)
		xwrite(t, f.dir, "key.der", x.keyPEM(t, "pkcs8der"))
		xwrite(t, f.dir, "cert.der", x.leaf().Raw)
		xwrite(t, f.dir, "inter.der", x.inters()[0].Raw)
		xwrite(t, f.dir, "root.der", x.root().Raw)
		r := BuildRequest{PrivateKeyPath: filepath.Join(f.dir, "key.der"), CertPath: filepath.Join(f.dir, "cert.der"),
			IntermediatePath: filepath.Join(f.dir, "inter.der"), RootPath: filepath.Join(f.dir, "root.der"), Password: xpw, OutputPath: f.out, Overwrite: true}
		res, err := Build(ctx, r)
		if err != nil || res.Verification.ChainLength != 3 {
			t.Fatalf("%v %+v", err, res.Verification)
		}
		// DER concatenado de varios certificados en un solo archivo
		xwrite(t, f.dir, "inter2.der", append(append([]byte{}, x.inters()[0].Raw...), x.root().Raw...))
		r.IntermediatePath, r.RootPath = filepath.Join(f.dir, "inter2.der"), ""
		if res, err = Build(ctx, r); err != nil || res.Verification.ChainLength != 3 {
			t.Errorf("DER concatenado: %v %+v", err, res.Verification.ChainLength)
		}
		// llave EC DER tradicional
		ex := newXChain(t, "ec", 1)
		fe := ex.files(t)
		der, _ := x509.MarshalECPrivateKey(ex.leafKey.(*ecdsa.PrivateKey))
		xwrite(t, fe.dir, "ec.der", der)
		re := fe.req("a", "modern", true)
		re.PrivateKeyPath = filepath.Join(fe.dir, "ec.der")
		if _, err := Build(ctx, re); err != nil {
			t.Errorf("EC DER: %v", err)
		}
	})
	t.Run("llave cifrada PKCS#8 (openssl) y PEM tradicional cifrado", func(t *testing.T) {
		for _, kind := range []string{"rsa", "ec"} {
			x := newXChain(t, kind, 1)
			f := x.files(t)
			plain := f.key
			enc8 := filepath.Join(f.dir, "enc8.pem")
			if o, err := xrun(t, []string{"KP=kpass-123"}, ossl, "pkcs8", "-topk8", "-v2", "aes-256-cbc", "-in", plain, "-out", enc8, "-passout", "env:KP"); err != nil {
				t.Fatalf("openssl pkcs8: %v %s", err, o)
			}
			encLegacy := filepath.Join(f.dir, "enc_legacy.pem")
			var o string
			var err error
			if kind == "rsa" {
				o, err = xrun(t, []string{"KP=kpass-123"}, ossl, "rsa", "-in", plain, "-aes256", "-traditional", "-out", encLegacy, "-passout", "env:KP")
			} else {
				o, err = xrun(t, []string{"KP=kpass-123"}, ossl, "ec", "-in", plain, "-aes256", "-traditional", "-out", encLegacy, "-passout", "env:KP")
			}
			if err != nil {
				if kind != "ec" {
					t.Fatalf("openssl legacy enc: %v %s", err, o)
				}
				t.Logf("openssl no permite EC tradicional cifrado: %s", firstLines(o, 1))
				encLegacy = enc8
			}
			for name, kp := range map[string]string{"pkcs8": enc8, "legacy": encLegacy} {
				r := f.req("a", "modern", true)
				r.PrivateKeyPath = kp
				// sin frase
				if _, err := Build(ctx, r); err == nil || !strings.Contains(err.Error(), "frase de paso") {
					t.Errorf("%s/%s sin frase: %v", kind, name, err)
				}
				// frase incorrecta
				r.KeyPassphrase = "otra"
				if _, err := Build(ctx, r); err == nil || !strings.Contains(err.Error(), "incorrecta") {
					t.Errorf("%s/%s frase incorrecta: %v", kind, name, err)
				}
				// frase correcta
				r.KeyPassphrase = "kpass-123"
				if res, err := Build(ctx, r); err != nil || !res.Verification.OK {
					t.Errorf("%s/%s frase correcta: %v %+v", kind, name, err, res.Verification.Checks)
				}
			}
		}
	})
	t.Run("CRLF en los PEM", func(t *testing.T) {
		x := newXChain(t, "rsa", 1)
		f := x.files(t)
		crlf := func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n")) }
		xwrite(t, f.dir, "key.pem", crlf(x.keyPEM(t, "pkcs8")))
		xwrite(t, f.dir, "cert.pem", crlf(xpemCerts(x.leaf())))
		xwrite(t, f.dir, "inter.pem", crlf(xpemCerts(x.inters()...)))
		xwrite(t, f.dir, "root.pem", crlf(xpemCerts(x.root())))
		res, err := Build(ctx, f.req("a", "modern", true))
		if err != nil || res.Verification.ChainLength != 3 {
			t.Fatalf("CRLF: %v", err)
		}
	})
	t.Run("PEM con texto previo (Bag Attributes de openssl)", func(t *testing.T) {
		x := newXChain(t, "rsa", 1)
		f := x.files(t)
		src := filepath.Join(f.dir, "src.p12")
		chain := xwrite(t, f.dir, "chain.pem", xpemCerts(x.inters()[0], x.root()))
		if o, err := xrun(t, []string{"XPW=" + xpw}, ossl, "pkcs12", "-export", "-inkey", f.key, "-in", f.leaf, "-certfile", chain, "-name", "orig", "-out", src, "-passout", "env:XPW"); err != nil {
			t.Fatalf("%v %s", err, o)
		}
		all, err := xrun(t, []string{"XPW=" + xpw}, ossl, "pkcs12", "-in", src, "-nodes", "-passin", "env:XPW")
		if err != nil {
			t.Fatalf("%v %s", err, all)
		}
		if !strings.Contains(all, "Bag Attributes") {
			t.Fatalf("openssl no emitió Bag Attributes:\n%s", firstLines(all, 5))
		}
		// un solo archivo con llave + hoja + cadena, con "Bag Attributes" y "subject=/issuer="
		bag := xwrite(t, f.dir, "todo.pem", []byte(all))
		r := BuildRequest{PrivateKeyPath: bag, CertPath: bag, Password: xpw, OutputPath: f.out, Alias: "desdetodo"}
		res, err := Build(ctx, r)
		if err != nil || res.Verification.ChainLength != 3 {
			t.Fatalf("archivo único con Bag Attributes: %v %+v", err, res.Verification.ChainLength)
		}
		// inspección
		fi, err := Inspect(bag, "")
		t.Logf("Inspect(todo.pem) -> kind=%s certs=%d err=%v", fi.Kind, len(fi.Certs), err)
	})
	t.Run("ruta de salida con espacios y acentos; sobrescritura", func(t *testing.T) {
		x := newXChain(t, "rsa", 1)
		f := x.files(t)
		out := filepath.Join(f.dir, "carpeta con espacios", "áéí ñ", "salida final.p12")
		r := f.req("a", "modern", true)
		r.OutputPath = out
		res, err := Build(ctx, r)
		if err != nil || res.OutputPath != out {
			t.Fatalf("%v", err)
		}
		if kt := FindKeytool(); kt != "" && !res.Verification.KeytoolOK {
			t.Errorf("keytool no pudo con la ruta con espacios/acentos: %s", res.Verification.KeytoolOutput)
		}
		// sobrescritura: ¿silenciosa?
		os.WriteFile(out, []byte("contenido previo"), 0o600)
		res2, err := Build(ctx, r)
		b, _ := os.ReadFile(out)
		t.Logf("sobrescritura de un archivo existente: err=%v, ahora %d bytes, avisos=%v", err, len(b), res2.Warnings)
		if err == nil && len(res2.Warnings) == 0 {
			t.Logf("COMPORTAMIENTO: se sobrescribe un .p12 existente SIN aviso")
		}
	})
	t.Run("contraseña con caracteres no ASCII y comillas", func(t *testing.T) {
		kt := needKeytool(t)
		x := newXChain(t, "rsa", 1)
		f := x.files(t)
		for _, pw := range []string{"ñandú-€-123", `a"b'c d;e&f|g<h>`, "日本語パスワード", "p%ss$w0rd`x`"} {
			r := f.req("a", "modern", true)
			r.Password = pw
			res, err := Build(ctx, r)
			if err != nil {
				t.Errorf("%q: %v", pw, err)
				continue
			}
			// Los JDK recientes rechazan contraseñas no ASCII en PKCS#12 con PBES2 ("Password is not ASCII"):
			// es una limitación de keytool, no del archivo generado, así que solo se registra.
			jdkNoASCII := func(out string) bool { return strings.Contains(out, "Password is not ASCII") }
			if !res.Verification.KeytoolOK {
				if jdkNoASCII(res.Verification.KeytoolOutput) {
					t.Logf("LIMITACIÓN DEL JDK: este keytool no acepta la contraseña no ASCII %q", pw)
				} else {
					t.Errorf("keytool no abre con la contraseña %q: %s", pw, res.Verification.KeytoolOutput)
				}
			}
			if o, err := xrun(t, []string{"XPW=" + pw}, kt, "-list", "-keystore", f.out, "-storetype", "PKCS12", "-storepass:env", "XPW"); err != nil {
				if jdkNoASCII(o) {
					t.Logf("LIMITACIÓN DEL JDK (keytool externo) con %q", pw)
				} else {
					t.Errorf("keytool externo con %q: %v %s", pw, err, o)
				}
			}
			if o, err := xrun(t, []string{"XPW=" + pw}, ossl, "pkcs12", "-in", f.out, "-nokeys", "-passin", "env:XPW"); err != nil {
				// openssl en Windows convierte el entorno desde la página de códigos ANSI, no UTF-8: no es concluyente
				t.Logf("openssl con %q (entorno ANSI en Windows): %v %s", pw, err, firstLines(o, 1))
			}
		}
	})
	t.Run("Verify con contraseña incorrecta y archivo corrupto", func(t *testing.T) {
		x := newXChain(t, "rsa", 1)
		f := x.files(t)
		if _, err := Build(ctx, f.req("a", "modern", true)); err != nil {
			t.Fatal(err)
		}
		_, err := Verify(ctx, f.out, "Incorrecta-123", "a")
		if err == nil || strings.Contains(err.Error(), "Incorrecta-123") || strings.Contains(err.Error(), xpw) {
			t.Errorf("%v", err)
		}
		bad := xwrite(t, f.dir, "bad.p12", []byte("no es un p12"))
		if _, err := Verify(ctx, bad, xpw, ""); err == nil {
			t.Error("archivo corrupto debía fallar")
		}
		if _, err := Verify(ctx, filepath.Join(f.dir, "no-existe.p12"), xpw, ""); err == nil {
			t.Error("archivo inexistente debía fallar")
		}
	})
	t.Run("certificado vencido y por vencer: solo aviso", func(t *testing.T) {
		x := newXChain(t, "rsa", 0)
		now := time.Now()
		k := xgen(t, "rsa")
		tm := &x509.Certificate{SerialNumber: big.NewInt(5), Subject: pkix.Name{CommonName: "exp"}, NotBefore: now.Add(-100 * 24 * time.Hour), NotAfter: now.Add(-24 * time.Hour)}
		c := xcert(t, tm, x.root(), k.Public(), x.keys[0])
		d := t.TempDir()
		der, _ := x509.MarshalPKCS8PrivateKey(k)
		r := BuildRequest{PrivateKeyPath: xwrite(t, d, "k.pem", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
			CertPath: xwrite(t, d, "c.pem", xpemCerts(c)), RootPath: xwrite(t, d, "r.pem", xpemCerts(x.root())), Password: xpw, OutputPath: filepath.Join(d, "o.p12")}
		res, err := Build(ctx, r)
		if err != nil || !anyContains(res.Warnings, "VENCIDO") {
			t.Errorf("%v %v", err, res.Warnings)
		}
	})
}

func anyContains(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- la contraseña no va en la línea de comandos

// Compila un "keytool" falso que registra sus argumentos y entorno, y delega en el keytool real.
func TestXVal_PasswordNotInKeytoolCommandLine(t *testing.T) {
	real := needKeytool(t)
	if runtime.GOOS != "windows" {
		t.Skip("el proxy de keytool de esta prueba se compila como keytool.exe (Windows)")
	}
	dir := t.TempDir()
	logf := filepath.Join(dir, "args.log")
	src := filepath.Join(dir, "proxy", "main.go")
	os.MkdirAll(filepath.Dir(src), 0o755)
	os.WriteFile(filepath.Join(dir, "proxy", "go.mod"), []byte("module proxy\ngo 1.21\n"), 0o644)
	os.WriteFile(src, []byte(`package main
import ("os";"os/exec";"strings")
func main(){
 f,_:=os.OpenFile(os.Getenv("PROXY_LOG"),os.O_APPEND|os.O_CREATE|os.O_WRONLY,0o644)
 f.WriteString("ARGS: "+strings.Join(os.Args[1:]," ")+"\n"); f.Close()
 c:=exec.Command(os.Getenv("REAL_KEYTOOL"),os.Args[1:]...); c.Stdout=os.Stdout; c.Stderr=os.Stderr; c.Stdin=os.Stdin
 if err:=c.Run();err!=nil{ if e,ok:=err.(*exec.ExitError);ok{os.Exit(e.ExitCode())}; os.Exit(1)}
}
`), 0o644)
	bin := filepath.Join(dir, "jdk", "bin")
	os.MkdirAll(bin, 0o755)
	cmd := exec.Command("go", "build", "-o", filepath.Join(bin, "keytool.exe"), ".")
	cmd.Dir = filepath.Join(dir, "proxy")
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("no se pudo compilar el proxy: %v %s", err, o)
	}
	t.Setenv("JAVA_HOME", filepath.Join(dir, "jdk"))
	t.Setenv("PROXY_LOG", logf)
	t.Setenv("REAL_KEYTOOL", real)
	if FindKeytool() != filepath.Join(dir, "jdk", "bin", "keytool.exe") {
		t.Skipf("FindKeytool no usó el proxy: %s", FindKeytool())
	}
	x := newXChain(t, "rsa", 1)
	f := x.files(t)
	r := f.req("a", "modern", true)
	r.Password = "Pw-NoDebeVerse-987"
	res, err := Build(context.Background(), r)
	if err != nil || !res.Verification.KeytoolOK {
		t.Fatalf("%v %s", err, res.Verification.KeytoolOutput)
	}
	log, _ := os.ReadFile(logf)
	t.Logf("líneas de comando registradas:\n%s", log)
	if strings.Contains(string(log), "Pw-NoDebeVerse-987") {
		t.Fatal("LA CONTRASEÑA APARECE EN LA LÍNEA DE COMANDOS DE keytool")
	}
	if !strings.Contains(string(log), "-storepass:env") {
		t.Error("no se usó -storepass:env")
	}
	if strings.Contains(res.Verification.KeytoolOutput, "Pw-NoDebeVerse-987") {
		t.Error("la salida de keytool contiene la contraseña")
	}
}

// El archivo .p12 se escribe con permisos restrictivos (0600 en POSIX).
func TestXVal_OutputFileMode(t *testing.T) {
	x := newXChain(t, "ec", 1)
	f := x.files(t)
	if _, err := Build(context.Background(), f.req("a", "modern", true)); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(f.out)
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
		t.Errorf("permisos del .p12: %v", st.Mode().Perm())
	}
}

// BUG (baja): Verify usa pkcs12.ToPEM, que no entiende llaves Ed25519: un .p12 válido (keytool lo acepta y lista
// PrivateKeyEntry) se informa como "trustedCertEntry" sin alias y la verificación global queda en ERROR.
func TestEd25519KeyReportedAsTrustedCertEntry(t *testing.T) {
	kt := needKeytool(t)
	x := newXChain(t, "ed25519", 1)
	f := x.files(t)
	res, err := Build(context.Background(), f.req("mialias", "modern", true))
	if err != nil {
		t.Fatal(err)
	}
	out, kerr := xrun(t, []string{"XPW=" + xpw}, kt, "-list", "-v", "-keystore", f.out, "-storetype", "PKCS12", "-storepass:env", "XPW")
	ki := ParseKeytool(out)
	t.Logf("keytool: err=%v %+v", kerr, ki)
	if kerr == nil && ki.EntryType == "PrivateKeyEntry" && !res.Verification.OK {
		t.Errorf("keytool acepta el .p12 (PrivateKeyEntry, alias %q) pero la app lo marca como no verificado: entrada=%q alias=%q",
			ki.Alias, res.Verification.EntryType, res.Verification.Alias)
	}
}

// BUG (baja): un alias con caracteres no ASCII queda mal codificado en el friendlyName (keytool y openssl muestran
// bytes Latin-1 corruptos) y la verificación propia falla; debería rechazarse o codificarse bien (BMPString).
func TestNonASCIIAliasIsGarbled(t *testing.T) {
	kt := needKeytool(t)
	x := newXChain(t, "rsa", 1)
	f := x.files(t)
	_, err := Build(context.Background(), f.req("ñandú", "modern", true))
	if err == nil || !strings.Contains(err.Error(), "ASCII") {
		t.Fatalf("un alias no ASCII debe rechazarse con un error que explique ASCII: %v", err)
	}
	_ = kt
}

// BUG (baja): si el mismo certificado llega por dos archivos (cadena completa en el cert hoja y de nuevo en el
// intermedio) el aviso dice "no forma parte de la cadena y se omitió" aunque SÍ está en la cadena.
func TestDuplicateCertMisleadingWarning(t *testing.T) {
	x := newXChain(t, "rsa", 1)
	f := x.files(t)
	xwrite(t, f.dir, "cert.pem", xpemCerts(x.leaf(), x.inters()[0]))
	res, err := Build(context.Background(), f.req("a", "modern", true))
	if err != nil {
		t.Fatal(err)
	}
	if anyContains(res.Warnings, "no forma parte de la cadena") {
		t.Errorf("aviso engañoso: %v (la cadena de %d incluye ese certificado)", res.Warnings, res.Verification.ChainLength)
	}
}

// BUG (media): Build sobrescribe sin aviso un .p12 existente (pérdida de un almacén previo con otras entradas).
func TestBuildOverwritesExistingOutputSilently(t *testing.T) {
	x := newXChain(t, "rsa", 1)
	f := x.files(t)
	os.WriteFile(f.out, []byte("almacén previo del usuario"), 0o600)
	r := f.req("a", "modern", true)
	r.Overwrite = false
	if _, err := Build(context.Background(), r); err == nil || !strings.Contains(err.Error(), "ya existe") {
		t.Fatalf("sin Overwrite debía fallar con \"el archivo ya existe\": %v", err)
	}
	if b, _ := os.ReadFile(f.out); string(b) != "almacén previo del usuario" {
		t.Fatal("el archivo previo fue modificado")
	}
	r.Overwrite = true
	if _, err := Build(context.Background(), r); err != nil {
		t.Fatalf("con Overwrite debía poder reemplazar: %v", err)
	}
}
