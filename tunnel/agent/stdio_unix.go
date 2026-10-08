//go:build unix

package agent

import (
	"errors"
	"os/exec"
	"syscall"
)

const stdioSupported = true

func shellCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}

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
