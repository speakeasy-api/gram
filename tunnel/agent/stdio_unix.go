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

// reapGroupMembers collects members of the server's process group that
// exited after being reparented to the agent, as happens when it runs as
// PID 1. An unreaped zombie stays in its group, so the group would never look
// gone. Call it only after cmd.Wait has reaped the leader.
func reapGroupMembers(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	for {
		pid, err := syscall.Wait4(-cmd.Process.Pid, nil, syscall.WNOHANG, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil || pid <= 0 {
			return
		}
	}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
