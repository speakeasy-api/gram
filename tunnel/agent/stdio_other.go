//go:build !unix

package agent

import "os/exec"

// Without process groups a server's children could outlive it.
const stdioSupported = false

func shellCommand(command string) *exec.Cmd {
	return exec.Command("cmd", "/C", command)
}

func configureProcessGroup(*exec.Cmd, bool) {}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func processGroupAlive(_ *exec.Cmd, exited <-chan struct{}) bool {
	select {
	case <-exited:
		return false
	default:
		return true
	}
}

func killProcessGroup(cmd *exec.Cmd) {
	terminateProcessGroup(cmd)
}
