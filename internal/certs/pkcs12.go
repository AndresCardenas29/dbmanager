package certs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// Check es una comprobación individual.
type Check struct {
	Name     string
	OK       bool
	Detail   string
	Severity string // ok | warning | error
}

// BuildRequest son los parámetros para generar un .p12.
type BuildRequest struct {
	PrivateKeyPath   string
	KeyPassphrase    string
	CertPath         string
	IntermediatePath string
	RootPath         string
	Alias            string
	Password         string
	OutputPath       string
	Format           string // modern | legacy
	Overwrite        bool   // permitir sobrescribir OutputPath si existe
}

// Verification es el resultado de verificar un .p12.
type Verification struct {
	OK               bool
	EntryType        string
	Alias            string
	ChainLength      int
	Chain            []Summary
	KeyMatchesCert   bool
	Checks           []Check
	KeytoolAvailable bool
	KeytoolOK        bool
	KeytoolOutput    string
}

// Result es el resultado de Build.
type Result struct {
	OutputPath   string
	Verification Verification
	Warnings     []string
	Leaf         Summary
}

// MinPasswordLen es el mínimo de keytool para almacenes PKCS12.
const MinPasswordLen = 6

// ChainResult es una cadena ordenada con avisos.
type ChainResult struct {
	Chain    []*x509.Certificate // hoja, intermedios..., (raíz)
	Warnings []string
}

// OrderChain ordena la cadena a partir de la hoja usando el resto como pool. Devuelve error si
// la hoja no está firmada por ningún certificado dado (falta el intermedio) y no es autofirmada.
func OrderChain(leaf *x509.Certificate, pool []*x509.Certificate) (ChainResult, error) {
	res := ChainResult{Chain: []*x509.Certificate{leaf}}
	used := map[int]bool{}
	cur := leaf
	for !IsSelfSigned(cur) {
		found := -1
		for i, c := range pool {
			if used[i] || c.Equal(cur) {
				continue
			}
			if cur.CheckSignatureFrom(c) == nil {
				found = i
				break
			}
		}
		if found < 0 {
			break
		}
		used[found] = true
		cur = pool[found]
		res.Chain = append(res.Chain, cur)
	}
	for i, c := range pool {
		if !used[i] && !c.Equal(leaf) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("El certificado «%s» no forma parte de la cadena y se omitió.", cnOrSubject(c)))
		}
	}
	if len(res.Chain) == 1 && !IsSelfSigned(leaf) {
		return res, fmt.Errorf("falta el certificado intermedio: «%s» no está firmado por ninguno de los certificados proporcionados (emisor: %s)", cnOrSubject(leaf), leaf.Issuer.String())
	}
	return res, nil
}

func cnOrSubject(c *x509.Certificate) string {
	if c.Subject.CommonName != "" {
		return c.Subject.CommonName
	}
	return c.Subject.String()
}

// expiryWarnings genera avisos de vencimiento.
func expiryWarnings(chain []*x509.Certificate, now time.Time) []string {
	var w []string
	for _, c := range chain {
		switch {
		case now.After(c.NotAfter):
			w = append(w, fmt.Sprintf("El certificado «%s» está VENCIDO desde %s.", cnOrSubject(c), c.NotAfter.Format("2006-01-02")))
		case now.Before(c.NotBefore):
			w = append(w, fmt.Sprintf("El certificado «%s» aún no es válido (desde %s).", cnOrSubject(c), c.NotBefore.Format("2006-01-02")))
		case c.NotAfter.Sub(now) < 30*24*time.Hour:
			w = append(w, fmt.Sprintf("El certificado «%s» vence en menos de 30 días (%s).", cnOrSubject(c), c.NotAfter.Format("2006-01-02")))
		}
	}
	return w
}

func loadOptionalCerts(path string) ([]*x509.Certificate, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	return LoadCerts(path)
}

// Build valida las entradas, genera el .p12 y lo verifica.
func Build(ctx context.Context, req BuildRequest) (Result, error) {
	var res Result
	alias := strings.TrimSpace(req.Alias)
	if alias == "" {
		alias = "tomcat"
	}
	if len([]rune(req.Password)) < MinPasswordLen {
		return res, fmt.Errorf("la contraseña del .p12 debe tener al menos %d caracteres (requisito de keytool)", MinPasswordLen)
	}
	if strings.TrimSpace(req.OutputPath) == "" {
		return res, errors.New("indique la ruta de salida del .p12")
	}
	if err := ValidateAlias(alias); err != nil {
		return res, err
	}
	if st, err := os.Stat(req.OutputPath); err == nil && !req.Overwrite {
		if st.IsDir() {
			return res, errors.New("la ruta de salida es una carpeta, no un archivo")
		}
		return res, fmt.Errorf("el archivo ya existe: %s (elija otro nombre o confirme que desea reemplazarlo)", req.OutputPath)
	}
	if strings.TrimSpace(req.PrivateKeyPath) == "" || strings.TrimSpace(req.CertPath) == "" {
		return res, errors.New("la llave privada y el certificado son obligatorios")
	}
	var enc *pkcs12.Encoder
	switch req.Format {
	case "", "modern":
		enc = pkcs12.Modern
	case "legacy":
		enc = pkcs12.LegacyDES
	default:
		return res, errors.New("formato inválido: use modern o legacy")
	}

	ki, err := LoadKey(req.PrivateKeyPath, req.KeyPassphrase)
	if err != nil {
		return res, err
	}
	certs, err := LoadCerts(req.CertPath)
	if err != nil {
		return res, err
	}
	inter, err := loadOptionalCerts(req.IntermediatePath)
	if err != nil {
		return res, fmt.Errorf("intermedio: %w", err)
	}
	root, err := loadOptionalCerts(req.RootPath)
	if err != nil {
		return res, fmt.Errorf("raíz: %w", err)
	}

	// La hoja es el certificado cuya llave pública corresponde a la llave privada.
	var leaf *x509.Certificate
	pool := []*x509.Certificate{}
	for _, c := range certs {
		if leaf == nil && KeyMatchesCert(ki.Key, c) {
			leaf = c
		} else {
			pool = append(pool, c)
		}
	}
	if leaf == nil {
		return res, errors.New("la llave privada NO corresponde a ningún certificado del archivo de certificado")
	}
	pool = append(pool, inter...)
	pool = append(pool, root...)
	pool = dedupe(pool, leaf)
	oc, err := OrderChain(leaf, pool)
	if err != nil {
		return res, err
	}
	chain := oc.Chain
	res.Warnings = append(res.Warnings, oc.Warnings...)

	// Aviso si el orden de los archivos dado no era el de la cadena.
	given := dedupe(append(append([]*x509.Certificate{}, inter...), root...), leaf)
	var expected []*x509.Certificate
	for _, c := range chain[1:] {
		for _, g := range given {
			if g.Equal(c) {
				expected = append(expected, c)
				break
			}
		}
	}
	for i := range expected {
		if i < len(given) && !expected[i].Equal(given[i]) {
			res.Warnings = append(res.Warnings, "Los certificados se entregaron fuera de orden (p. ej. la raíz antes del intermedio); se reordenó la cadena automáticamente.")
			break
		}
	}
	// Raíz explícita: debe ser autofirmada y formar parte de la cadena.
	if len(root) > 0 {
		last := chain[len(chain)-1]
		if !IsSelfSigned(root[0]) {
			return res, fmt.Errorf("el certificado raíz «%s» no es autofirmado", cnOrSubject(root[0]))
		}
		if !last.Equal(root[0]) {
			return res, fmt.Errorf("la cadena no llega hasta la raíz indicada «%s»: el último certificado alcanzado es «%s»", cnOrSubject(root[0]), cnOrSubject(last))
		}
	}
	if len(inter) > 0 && len(chain) < 2 {
		return res, errors.New("el certificado no está firmado por el intermedio indicado")
	}
	res.Warnings = append(res.Warnings, expiryWarnings(chain, time.Now())...)
	res.Leaf = Summarize(leaf)

	data, err := enc.EncodeWithAlias(ki.Key, leaf, chain[1:], req.Password, alias)
	if err != nil {
		return res, fmt.Errorf("no se pudo generar el PKCS#12: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0o755); err != nil {
		return res, err
	}
	if err := os.WriteFile(req.OutputPath, data, 0o600); err != nil {
		return res, fmt.Errorf("no se pudo escribir %s: %w", req.OutputPath, err)
	}
	res.OutputPath = req.OutputPath
	v, err := Verify(ctx, req.OutputPath, req.Password, alias)
	if err != nil {
		return res, fmt.Errorf("el archivo se generó pero la verificación falló: %w", err)
	}
	// longitud esperada
	want := len(chain)
	v.Checks = append(v.Checks, Check{Name: "Longitud de cadena esperada", OK: v.ChainLength == want, Detail: fmt.Sprintf("esperada %d, encontrada %d", want, v.ChainLength)})
	if v.ChainLength != want {
		v.OK = false
	}
	res.Verification = v
	return res, nil
}

// aliasFromBags obtiene el alias (friendlyName, o localKeyId como lo muestra keytool) de la entrada de llave.
func aliasFromBags(data []byte, password string) (alias string, hasKey bool) {
	bags, err := pkcs12.ListBags(data, password)
	if err != nil {
		return "", false
	}
	for _, b := range bags {
		if b.Kind != "key" {
			continue
		}
		hasKey = true
		if b.FriendlyName != "" {
			return b.FriendlyName, true
		}
		if b.LocalKeyID != "" {
			return strings.ToLower(b.LocalKeyID), true
		}
	}
	return "", hasKey
}

// Verify decodifica el .p12 y comprueba lo que revisaría keytool.
func Verify(ctx context.Context, path, password, expectedAlias string) (Verification, error) {
	var v Verification
	data, err := os.ReadFile(path)
	if err != nil {
		return v, fmt.Errorf("no se puede leer el .p12: %w", err)
	}
	key, cert, cas, err := pkcs12.DecodeChain(data, password)
	if err != nil {
		if errors.Is(err, pkcs12.ErrIncorrectPassword) {
			return v, errors.New("contraseña incorrecta para el .p12")
		}
		return v, fmt.Errorf("no se pudo leer el .p12: %w", err)
	}
	alias, hasKey := aliasFromBags(data, password)
	v.Alias = alias
	v.EntryType = "trustedCertEntry"
	if hasKey {
		v.EntryType = "PrivateKeyEntry"
	}
	add := func(name string, ok bool, detail string) {
		sev := "ok"
		if !ok {
			sev = "error"
		}
		v.Checks = append(v.Checks, Check{Name: name, OK: ok, Detail: detail, Severity: sev})
	}
	warn := func(name, detail string) {
		v.Checks = append(v.Checks, Check{Name: name, OK: true, Detail: detail, Severity: "warning"})
	}
	add("Tipo de entrada", v.EntryType == "PrivateKeyEntry", v.EntryType)
	if expectedAlias != "" {
		switch {
		case alias == expectedAlias:
			add("Alias", true, alias)
		case strings.EqualFold(alias, expectedAlias):
			add("Alias", true, fmt.Sprintf("%s (keytool lo muestra en minúsculas; difiere solo en mayúsculas de «%s»)", alias, expectedAlias))
		default:
			add("Alias", false, fmt.Sprintf("esperado «%s», encontrado «%s»", expectedAlias, alias))
		}
	}
	chain := []*x509.Certificate{cert}
	if cert != nil {
		pool := cas
		if oc, err := OrderChain(cert, pool); err == nil {
			chain = oc.Chain
		} else {
			chain = append(chain, cas...)
		}
	}
	for _, c := range chain {
		v.Chain = append(v.Chain, Summarize(c))
	}
	v.ChainLength = len(chain)
	lenOK := len(chain) >= 2 || (len(chain) == 1 && IsSelfSigned(chain[0]))
	add("Longitud de cadena", lenOK, strconv.Itoa(len(chain)))
	for _, c := range chain {
		switch d := validityNote(c, time.Now()); {
		case d != "":
			warn("Vigencia: "+cnOrSubject(c), d)
		}
	}
	if key != nil && cert != nil {
		v.KeyMatchesCert = KeyMatchesCert(key, cert)
	}
	add("Llave ↔ certificado", v.KeyMatchesCert, "")
	linksOK := true
	for i := 0; i+1 < len(chain); i++ {
		if chain[i].CheckSignatureFrom(chain[i+1]) != nil {
			linksOK = false
		}
	}
	add("Firmas de la cadena", linksOK, "")

	ok := true
	for _, c := range v.Checks {
		ok = ok && c.OK
	}
	v.OK = ok

	if kt := FindKeytool(); kt != "" {
		v.KeytoolAvailable = true
		out, kOK := runKeytool(ctx, kt, path, password, expectedAlias, v.ChainLength)
		v.KeytoolOutput, v.KeytoolOK = out, kOK
		add("keytool -list -v", kOK, "")
		v.OK = v.OK && kOK
	}
	return v, nil
}

// ---------- keytool ----------

func isKeytool(p string) bool {
	if p == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, p, "-help").Run() == nil
}

func keytoolName() string {
	if runtime.GOOS == "windows" {
		return "keytool.exe"
	}
	return "keytool"
}

// FindKeytool busca keytool: JAVA_HOME/bin, PATH, java_home de macOS, readlink de java en Linux y JDK comunes.
func FindKeytool() string {
	name := keytoolName()
	var cands []string
	if jh := os.Getenv("JAVA_HOME"); jh != "" {
		cands = append(cands, filepath.Join(jh, "bin", name))
	}
	if p, err := exec.LookPath("keytool"); err == nil {
		cands = append(cands, p)
	}
	switch runtime.GOOS {
	case "darwin":
		if out, err := exec.Command("/usr/libexec/java_home").Output(); err == nil {
			cands = append(cands, filepath.Join(strings.TrimSpace(string(out)), "bin", name))
		}
	case "linux":
		if out, err := exec.Command("sh", "-c", "readlink -f \"$(command -v java)\"").Output(); err == nil && len(bytes.TrimSpace(out)) > 0 {
			cands = append(cands, filepath.Join(filepath.Dir(strings.TrimSpace(string(out))), name))
		}
		for _, g := range []string{"/usr/lib/jvm/*/bin/keytool"} {
			m, _ := filepath.Glob(g)
			cands = append(cands, m...)
		}
	case "windows":
		for _, g := range []string{`C:\Program Files\Java\*\bin\keytool.exe`, `C:\Program Files\Eclipse Adoptium\*\bin\keytool.exe`, `C:\Program Files\Microsoft\*\bin\keytool.exe`} {
			m, _ := filepath.Glob(g)
			cands = append(cands, m...)
		}
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && isKeytool(c) {
			return c
		}
	}
	return ""
}

func runKeytool(ctx context.Context, kt, p12, password, alias string, chainLen int) (string, bool) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, kt, "-J-Duser.language=en", "-J-Duser.country=US", "-list", "-v", "-keystore", p12,
		"-storetype", "PKCS12", "-storepass:env", "DBM_P12_PASS")
	cmd.Env = append(os.Environ(), "DBM_P12_PASS="+password)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err != nil {
		return out, false
	}
	p := ParseKeytool(out)
	ok := p.EntryType == "PrivateKeyEntry" && (alias == "" || strings.EqualFold(p.Alias, alias)) && (chainLen == 0 || p.ChainLength == chainLen)
	return out, ok
}

// KeytoolInfo son los campos de interés de la salida de keytool.
type KeytoolInfo struct {
	Alias       string
	EntryType   string
	ChainLength int
}

// ParseKeytool extrae "Alias name:", "Entry type:" y "Certificate chain length:".
func ParseKeytool(out string) KeytoolInfo {
	var k KeytoolInfo
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "Alias name:"):
			k.Alias = strings.TrimSpace(strings.TrimPrefix(ln, "Alias name:"))
		case strings.HasPrefix(ln, "Entry type:"):
			k.EntryType = strings.TrimSpace(strings.TrimPrefix(ln, "Entry type:"))
		case strings.HasPrefix(ln, "Certificate chain length:"):
			k.ChainLength, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(ln, "Certificate chain length:")))
		}
	}
	return k
}

var _ = hex.EncodeToString

// ValidateAlias exige ASCII imprimible: otros caracteres se corrompen entre keytool/Java/OpenSSL.
func ValidateAlias(alias string) error {
	if len(alias) > 128 {
		return errors.New("el alias es demasiado largo (máx. 128 caracteres)")
	}
	for _, r := range alias {
		if r < 0x20 || r > 0x7e {
			return errors.New("el alias solo puede usar caracteres ASCII imprimibles (letras sin acentos, números y símbolos): keytool y Java corrompen los caracteres no ASCII, como acentos o ñ")
		}
	}
	return nil
}

// dedupe quita certificados repetidos (por SHA-256 del DER) y el propio leaf.
func dedupe(in []*x509.Certificate, leaf *x509.Certificate) []*x509.Certificate {
	seen := map[[32]byte]bool{}
	if leaf != nil {
		seen[sha256.Sum256(leaf.Raw)] = true
	}
	var out []*x509.Certificate
	for _, c := range in {
		h := sha256.Sum256(c.Raw)
		if !seen[h] {
			seen[h] = true
			out = append(out, c)
		}
	}
	return out
}

// validityNote describe un problema de vigencia ("" si está vigente con más de 30 días).
func validityNote(c *x509.Certificate, now time.Time) string {
	switch {
	case now.After(c.NotAfter):
		return fmt.Sprintf("El certificado está VENCIDO desde %s (el .p12 se genera igual, pero los clientes lo rechazarán).", c.NotAfter.Format("2006-01-02"))
	case now.Before(c.NotBefore):
		return fmt.Sprintf("El certificado aún no es válido (desde %s).", c.NotBefore.Format("2006-01-02"))
	case c.NotAfter.Sub(now) < 30*24*time.Hour:
		return fmt.Sprintf("El certificado vence en menos de 30 días (%s).", c.NotAfter.Format("2006-01-02"))
	}
	return ""
}
