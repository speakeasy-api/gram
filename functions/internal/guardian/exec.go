package guardian

import (
	"context"
	"os/exec"
)

func NewCommand(ctx context.Context, command string, args ...string) *exec.Cmd {
	// #nosec G204 -- This is a utility function that expects the caller to
	// provide a safe command and arguments.
	cmd := exec.CommandContext(ctx, command, args...)
	// 🚨🚨🚨🚨🚨
	// <NOTICE>
	// YOU MUST ALWAYS SET CMD.ENV TO A NON-NIL VALUE SO THE PROCESS DOES NOT
	// INHERIT THE PARENT PROCESS'S ENVIRONMENT.
	// 🚨🚨🚨🚨🚨
	cmd.Env = []string{}
	// 🚨🚨🚨🚨🚨
	// </NOTICE>
	// 🚨🚨🚨🚨🚨

	return cmd
}
