//go:build windows

package builder

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// setProcessGroup is a no-op on Windows: there is no Setpgid equivalent, and
// this package does not wire a job object, so only the direct child is
// killed on timeout (see killProcessGroup).
func setProcessGroup(_ *exec.Cmd) {}

// killProcessGroup kills only the direct child process.
//
// Caveat: unlike the Unix path (SIGKILL to the whole process group),
// grandchildren spawned by the builder binary survive. Keep builder timeouts
// short on Windows.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// stillActive is the Windows STILL_ACTIVE exit code (259): a process whose
// exit code is still this value has not exited.
const stillActive = 259

// processAlive reports whether pid still runs, via OpenProcess plus the exit
// code (stillActive means the process has not exited).
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
