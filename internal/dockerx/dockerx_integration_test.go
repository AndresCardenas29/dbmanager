//go:build integration

package dockerx_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dbmanager/internal/dockerx"
	"dbmanager/internal/testutil"
)

// Contenedor postgres de prueba propio de este paquete (dbmanager-test-dockerx-*).
func startBox(t *testing.T) (*dockerx.Client, testutil.Container) {
	t.Helper()
	name := testutil.RandName("dockerx")
	c, err := testutil.StartPlain(testutil.PlainOptions{Name: name, Engine: "postgres", Password: testutil.PGPassword})
	t.Cleanup(func() { testutil.RemoveByName(name) })
	if err != nil {
		t.Fatal(err)
	}
	cli, err := dockerx.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cli.Close() })
	return cli, c
}

func sh(t *testing.T, cli *dockerx.Client, name, script string) dockerx.ExecResult {
	t.Helper()
	r, err := cli.Exec(context.Background(), name, dockerx.ExecOptions{Cmd: []string{"sh", "-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestResolveHostAndPingOnThisMachine(t *testing.T) {
	h := dockerx.ResolveHost()
	t.Logf("ResolveHost=%q DOCKER_HOST=%q DOCKER_CONTEXT=%q", h, os.Getenv("DOCKER_HOST"), os.Getenv("DOCKER_CONTEXT"))
	cli, err := dockerx.New()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cli.Ping(ctx); err != nil {
		t.Fatalf("Ping con el host %q: %v", h, err)
	}
	v, err := cli.Version(ctx)
	if err != nil || v.Version == "" {
		t.Fatalf("%+v %v", v, err)
	}
}

// El guard de path traversal de CopyFromContainer: un archivo del contenedor cuyo nombre contiene "..\" (válido en Linux)
// no debe escribir fuera de la carpeta de destino en Windows.
func TestCopyFromContainerDoesNotEscapeDestination(t *testing.T) {
	cli, c := startBox(t)
	r := sh(t, cli, c.Name, `rm -rf /tmp/tr && mkdir -p /tmp/tr/sub && cd /tmp/tr && echo ok > ok.txt && echo in-sub > sub/s.txt && echo evil > 'a\..\..\evil.txt' && echo evil2 > '..\evil2.txt' && echo evil3 > '..\..\..\evil3.txt' && echo c > 'x:y.txt'`)
	if r.ExitCode != 0 {
		t.Fatalf("preparación: %+v", r)
	}
	base := t.TempDir()
	dest := filepath.Join(base, "dest")
	out, err := cli.CopyFromContainer(context.Background(), c.Name, "/tmp/tr", dest)
	t.Logf("CopyFromContainer -> %q err=%v", out, err)
	// pase lo que pase (error o copia), nada puede estar fuera de dest
	for _, esc := range []string{filepath.Join(base, "evil.txt"), filepath.Join(base, "evil2.txt"), filepath.Join(filepath.Dir(base), "evil3.txt"),
		filepath.Join(base, "dest", "..", "evil.txt")} {
		if _, err := os.Stat(esc); err == nil {
			t.Errorf("ESCAPE: se escribió fuera del destino: %s", esc)
			os.Remove(esc)
		}
	}
	filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err == nil && !strings.HasPrefix(p, base) {
			t.Errorf("fuera: %s", p)
		}
		return nil
	})
}

// Copia de ida y vuelta binaria (5 MB aleatorios): integridad, y la ruta devuelta.
func TestCopyRoundTripBinaryIntegrity(t *testing.T) {
	cli, c := startBox(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "datos.bin")
	data := make([]byte, 5<<20)
	rand.Read(data)
	os.WriteFile(src, data, 0o600)
	ctx := context.Background()
	if err := cli.CopyToContainer(ctx, c.Name, src, "/tmp"); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "salida")
	got, err := cli.CopyFromContainer(ctx, c.Name, "/tmp/datos.bin", out)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(out, "datos.bin") {
		t.Errorf("ruta devuelta: %s", got)
	}
	b, _ := os.ReadFile(got)
	if !bytes.Equal(b, data) {
		t.Error("el archivo copiado difiere del original")
	}
	// carpeta como origen de CopyToContainer: error claro
	if err := cli.CopyToContainer(ctx, c.Name, dir, "/tmp"); err == nil {
		t.Error("copiar una carpeta debía fallar")
	}
	// archivo inexistente en el contenedor
	if _, err := cli.CopyFromContainer(ctx, c.Name, "/tmp/no-existe", out); err == nil {
		t.Error("copiar un archivo inexistente debía fallar")
	}
	// Se sobrescribe sin aviso un archivo del host con el mismo nombre (documentado)
	os.WriteFile(got, []byte("viejo"), 0o600)
	if _, err := cli.CopyFromContainer(ctx, c.Name, "/tmp/datos.bin", out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(got); !bytes.Equal(b, data) {
		t.Error("no se sobrescribió")
	}
}

func TestExecExitCodesStdinAndRunOnce(t *testing.T) {
	cli, c := startBox(t)
	ctx := context.Background()
	r, err := cli.Exec(ctx, c.Name, dockerx.ExecOptions{Cmd: []string{"sh", "-c", "echo out; echo err 1>&2; exit 7"}})
	if err != nil || r.ExitCode != 7 || !strings.Contains(r.Stdout, "out") || !strings.Contains(r.Stderr, "err") {
		t.Errorf("%+v %v", r, err)
	}
	r, err = cli.Exec(ctx, c.Name, dockerx.ExecOptions{Cmd: []string{"cat"}, Stdin: strings.NewReader("hola ñandú\n")})
	if err != nil || strings.TrimSpace(r.Stdout) != "hola ñandú" {
		t.Errorf("stdin: %+v %v", r, err)
	}
	r, err = cli.Exec(ctx, c.Name, dockerx.ExecOptions{Cmd: []string{"sh", "-c", "echo $X"}, Env: []string{"X=a'b\"c"}})
	if err != nil || strings.TrimSpace(r.Stdout) != `a'b"c` {
		t.Errorf("env: %+v %v", r, err)
	}
	// exec con contexto cancelado
	cctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = cli.Exec(cctx, c.Name, dockerx.ExecOptions{Cmd: []string{"sleep", "20"}})
	if err == nil || time.Since(start) > 10*time.Second {
		t.Errorf("exec debía cancelarse por contexto: %v (%v)", err, time.Since(start))
	}
	// RunOnce: devuelve código y salida, y elimina el contenedor temporal
	code, logs, err := cli.RunOnce(ctx, dockerx.CreateOptions{Name: testutil.RandName("runonce"), Image: testutil.PostgresImage,
		Cmd: []string{"sh", "-c", "echo hola; exit 3"}})
	if err != nil || code != 3 || !strings.Contains(logs, "hola") {
		t.Errorf("RunOnce: code=%d logs=%q err=%v", code, logs, err)
	}
	if left := testutil.ListNames(testutil.Prefix + "runonce"); len(left) != 0 {
		t.Errorf("RunOnce dejó contenedores: %v", left)
	}
	// Remove idempotente / not found
	if err := cli.Remove(ctx, testutil.Prefix+"no-existe-xyz", false); err == nil || !dockerx.IsNotFound(err) {
		t.Errorf("IsNotFound: %v", err)
	}
	if _, err := cli.Inspect(ctx, testutil.Prefix+"no-existe-xyz"); !dockerx.IsNotFound(err) {
		t.Errorf("Inspect not found: %v", err)
	}
	// ExistsImage
	if ok, err := cli.ImageExists(ctx, testutil.PostgresImage); err != nil || !ok {
		t.Errorf("ImageExists: %v %v", ok, err)
	}
	if ok, _ := cli.ImageExists(ctx, "dbmanager-test/no-existe:0"); ok {
		t.Error("imagen inexistente")
	}
	// los nombres de variables de entorno se exponen, nunca los valores
	info, err := cli.Inspect(ctx, c.Name)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range info.EnvNames {
		if strings.Contains(e, "=") || strings.Contains(e, testutil.PGPassword) {
			t.Errorf("EnvNames expone valores: %q", e)
		}
	}
}
