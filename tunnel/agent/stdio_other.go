//go:build !unix

package agent

import "os/exec"

func shellCommand(command string) *exec.Cmd {
	return exec.Command("cmd", "/C", command)
}

func configureProcessGroup(*exec.Cmd) {}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// processGroupAlive falls back to the server process itself, since there is
// no process group to inspect here.
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
