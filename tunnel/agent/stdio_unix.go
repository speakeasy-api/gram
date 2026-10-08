//go:build unix

package agent

import (
	"errors"
	"os/exec"
	"syscall"
)

func shellCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}

// configureProcessGroup puts the server in its own process group so shutdown
// also reaches anything it spawned (e.g. the node process behind npx).
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}

// processGroupAlive reports whether any process remains in the server's group,
// including its leader before it is reaped.
func processGroupAlive(cmd *exec.Cmd, _ <-chan struct{}) bool {
	if cmd.Process == nil {
		return false
	}
	return !errors.Is(syscall.Kill(-cmd.Process.Pid, 0), syscall.ESRCH)
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
