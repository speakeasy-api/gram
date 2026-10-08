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

func killProcessGroup(cmd *exec.Cmd) {
	terminateProcessGroup(cmd)
}
