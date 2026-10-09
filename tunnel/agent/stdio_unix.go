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

// configureProcessGroup gives the server its own process group. With
// killOnAgentExit the server is also killed if the agent dies, where the
// platform supports it; that reaches only the direct child.
func configureProcessGroup(cmd *exec.Cmd, killOnAgentExit bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if killOnAgentExit {
		setParentDeathSignal(cmd.SysProcAttr)
	}
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
