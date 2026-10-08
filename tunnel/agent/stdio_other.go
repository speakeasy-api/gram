//go:build !unix

package agent

import "os/exec"

// stdioSupported is false here: without process groups the agent cannot
// guarantee a server's children stop with it, so stdio mode is refused.
const stdioSupported = false

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
