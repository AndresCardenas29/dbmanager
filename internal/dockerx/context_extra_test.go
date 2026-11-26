package dockerx

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// DOCKER_CONTEXT tiene prioridad sobre currentContext de config.json.
func TestHostFromContextEnvOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"a"}`), 0o644)
	writeCtx(t, dir, "a", "tcp://a:1")
	writeCtx(t, dir, "b", "tcp://b:2")
	t.Setenv("DOCKER_CONTEXT", "b")
	if got := hostFromContext(dir); got != "tcp://b:2" {
		t.Fatalf("DOCKER_CONTEXT ignorado: %q", got)
	}
	t.Setenv("DOCKER_CONTEXT", "default")
	if got := hostFromContext(dir); got != "" {
		t.Fatalf("default debe ser vacío: %q", got)
	}
}

func TestHostFromContextCorruptFiles(t *testing.T) {
	t.Setenv("DOCKER_CONTEXT", "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{no es json`), 0o644)
	if hostFromContext(dir) != "" {
		t.Fatal("config corrupta debe dar vacío (no pánico)")
	}
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"x"}`), 0o644)
	writeCtx(t, dir, "x", "")
	if hostFromContext(dir) != "" {
		t.Fatal("endpoint vacío")
	}
	// meta.json corrupto
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"y"}`), 0o644)
	writeCtx(t, dir, "y", "tcp://y:1")
	// lo estropeamos
	matches, _ := filepath.Glob(filepath.Join(dir, "contexts", "meta", "*", "meta.json"))
	for _, m := range matches {
		os.WriteFile(m, []byte("{{{"), 0o644)
	}
	if hostFromContext(dir) != "" {
		t.Fatal("meta corrupto debe dar vacío")
	}
}

// DOCKER_CONFIG redefine el directorio; DOCKER_HOST gana siempre.
func TestResolveHostUsesDockerConfig(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"ctx1"}`), 0o644)
	writeCtx(t, dir, "ctx1", "unix:///tmp/ctx1.sock")
	t.Setenv("DOCKER_CONFIG", dir)
	if got := ResolveHost(); got != "unix:///tmp/ctx1.sock" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("DOCKER_HOST", "tcp://override:1")
	if got := ResolveHost(); got != "tcp://override:1" {
		t.Fatalf("DOCKER_HOST debe ganar: %q", got)
	}
}

// En esta máquina el contexto activo es desktop-linux (ver docs/ENVIRONMENT.md); solo se comprueba si existe.
func TestResolveHostRealMachineWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("solo Windows")
	}
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_CONFIG", "")
	home, _ := os.UserHomeDir()
	b, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	if err != nil {
		t.Skip("sin ~/.docker/config.json")
	}
	h := ResolveHost()
	t.Logf("config=%s -> host=%q", string(b), h)
}
