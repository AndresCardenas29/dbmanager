//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideWindow evita que aparezca una ventana de consola al lanzar el subproceso MCP.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
