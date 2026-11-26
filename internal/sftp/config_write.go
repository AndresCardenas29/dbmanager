package sftp

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteConfig escribe en base: users.conf, hooks/10-persist-host-keys.sh, keys/<usuario>/*.pub y
// hostkeys/ (las llaves de host solo se generan si no existen). Nunca escribe llaves privadas de cliente.
func WriteConfig(base string, users []UserCfg) (ConfigPaths, error) {
	p := Paths(base)
	for _, d := range []string{base, p.HooksDir, p.KeysDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return p, err
		}
	}
	if err := EnsureHostKeys(p.HostKeys); err != nil {
		return p, fmt.Errorf("no se pudieron generar las llaves de host: %w", err)
	}
	if err := os.WriteFile(p.UsersConf, []byte(UsersConf(users)), 0o644); err != nil {
		return p, err
	}
	if err := os.WriteFile(filepath.Join(p.HooksDir, "10-persist-host-keys.sh"), []byte(HookScript(users)), 0o755); err != nil {
		return p, err
	}
	// Se regenera por completo el árbol de llaves públicas.
	if err := os.RemoveAll(p.KeysDir); err != nil {
		return p, err
	}
	for _, u := range users {
		ud := filepath.Join(p.KeysDir, u.Name)
		if err := os.MkdirAll(ud, 0o755); err != nil {
			return p, err
		}
		for i, k := range u.PublicKeys {
			if err := os.WriteFile(filepath.Join(ud, fmt.Sprintf("key%d.pub", i+1)), []byte(k+"\n"), 0o644); err != nil {
				return p, err
			}
		}
	}
	return p, nil
}
