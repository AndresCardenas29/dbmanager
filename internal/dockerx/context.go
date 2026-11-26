package dockerx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// ResolveHost determina el endpoint de Docker a usar.
// Prioridad: DOCKER_HOST; si no, el contexto activo de ~/.docker/config.json;
// si no, "" (el SDK usa su valor por defecto).
func ResolveHost() string {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := os.Getenv("DOCKER_CONFIG")
	if dir == "" {
		dir = filepath.Join(home, ".docker")
	}
	return hostFromContext(dir)
}

// hostFromContext lee currentContext en <dir>/config.json y el endpoint docker de su meta.json.
func hostFromContext(dir string) string {
	if name := os.Getenv("DOCKER_CONTEXT"); name != "" {
		return metaHost(dir, name)
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		CurrentContext string `json:"currentContext"`
	}
	if json.Unmarshal(b, &cfg) != nil || cfg.CurrentContext == "" || cfg.CurrentContext == "default" {
		return ""
	}
	return metaHost(dir, cfg.CurrentContext)
}

func metaHost(dir, name string) string {
	if name == "" || name == "default" {
		return ""
	}
	sum := sha256.Sum256([]byte(name))
	b, err := os.ReadFile(filepath.Join(dir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json"))
	if err != nil {
		return ""
	}
	var meta struct {
		Endpoints map[string]struct {
			Host string `json:"Host"`
		} `json:"Endpoints"`
	}
	if json.Unmarshal(b, &meta) != nil {
		return ""
	}
	return meta.Endpoints["docker"].Host
}
