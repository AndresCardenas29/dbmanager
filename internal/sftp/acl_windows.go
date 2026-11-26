//go:build windows

package sftp

import (
	"os/exec"
	"os/user"
)

// restrictFile deja el archivo accesible solo para el usuario actual (equivalente a chmod 600 en Windows);
// OpenSSH de Windows rechaza llaves privadas con ACL heredada demasiado abierta.
func restrictFile(path string) {
	u, err := user.Current()
	if err != nil {
		return
	}
	exec.Command("icacls", path, "/inheritance:r", "/grant:r", u.Username+":F").Run()
}
