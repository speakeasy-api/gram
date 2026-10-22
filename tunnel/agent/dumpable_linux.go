//go:build linux

package agent

import "golang.org/x/sys/unix"

// hideFromStdioServer marks the agent non-dumpable. A server runs as the
// agent's user, which would otherwise let it read /proc/<agent>/environ,
// TUNNEL_KEY included, or ptrace the agent. The flag is per process and exec
// resets it, so the server itself is unaffected.
func hideFromStdioServer() error {
	return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}
