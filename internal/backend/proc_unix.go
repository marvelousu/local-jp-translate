//go:build !windows

package backend

import (
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func interruptProcess(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

func killProcess(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
