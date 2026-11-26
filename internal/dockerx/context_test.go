package dockerx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func writeCtx(t *testing.T, dir, name, host string) {
	t.Helper()
	sum := sha256.Sum256([]byte(name))
	p := filepath.Join(dir, "contexts", "meta", hex.EncodeToString(sum[:]))
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "meta.json"), []byte(`{"Name":"`+name+`","Endpoints":{"docker":{"Host":"`+host+`"}}}`), 0o644)
}

func TestHostFromContext(t *testing.T) {
	t.Setenv("DOCKER_CONTEXT", "")
	dir := t.TempDir()
	if hostFromContext(dir) != "" {
		t.Fatal("sin config debe ser vacío")
	}
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"desktop-linux"}`), 0o644)
	if hostFromContext(dir) != "" {
		t.Fatal("sin meta debe ser vacío")
	}
	writeCtx(t, dir, "desktop-linux", "npipe:////./pipe/dockerDesktopLinuxEngine")
	if got := hostFromContext(dir); got != "npipe:////./pipe/dockerDesktopLinuxEngine" {
		t.Fatalf("got %q", got)
	}
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"default"}`), 0o644)
	if hostFromContext(dir) != "" {
		t.Fatal("default debe ser vacío")
	}
}

func TestResolveHostPriority(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://1.2.3.4:2375")
	if ResolveHost() != "tcp://1.2.3.4:2375" {
		t.Fatal("DOCKER_HOST debe tener prioridad")
	}
}
