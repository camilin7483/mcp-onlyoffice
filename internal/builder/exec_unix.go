//go:build !windows

package builder

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the child in its own process group so a timeout can
// kill the whole group (child plus grandchildren) with one signal.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup SIGKILLs the child's process group. It is the timeout path
// for execute: grandchildren die with the child.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// processAlive reports whether pid still runs (ESRCH/EPERM probe).
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
