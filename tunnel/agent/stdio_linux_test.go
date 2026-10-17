//go:build linux

package agent

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// subreaperTestEnv marks the child process that runs a test as a subreaper.
const subreaperTestEnv = "GRAM_TUNNEL_TEST_SUBREAPER"

func TestStdioServerCannotReadAgentEnvironment(t *testing.T) {
	t.Parallel()
	probe := filepath.Join(t.TempDir(), "probe")
	_, a := newStdioTestServerWithCommand(t, `if cat /proc/$PPID/environ >/dev/null 2>&1; then echo readable; else echo protected; fi >'`+probe+`'; read ignored`, 0)
	_, err := a.stdio.start(t.Context(), nil, principal{})
	require.NoError(t, err)

	var result []byte
	require.Eventually(t, func() bool {
		result, err = os.ReadFile(probe)
		return err == nil && len(result) > 0
	}, 10*time.Second, 20*time.Millisecond)
	require.Equal(t, "protected\n", string(result), "a server running as the agent's user must not read its environment")
}

// A subreaper stands in for running as PID 1: orphans of the server's process
// group are reparented to it.
func TestStdioBridgeReapsReparentedGroupMembers(t *testing.T) {
	if os.Getenv(subreaperTestEnv) != "1" {
		t.Parallel()
		// Becoming a subreaper changes the whole process, so the test runs in a child.
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestStdioBridgeReapsReparentedGroupMembers$", "-test.count=1")
		cmd.Env = append(os.Environ(), subreaperTestEnv+"=1")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return
	}
	require.NoError(t, unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0))

	// Closing stdin ends the shell; its sleep is reparented to this process.
	_, a := newStdioTestServerWithCommand(t, "sleep 300 & read ignored", 0)
	sess, err := a.stdio.start(t.Context(), nil, principal{})
	require.NoError(t, err)
	pgid := sess.cmd.Process.Pid

	sess.close()
	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
	}, 5*time.Second, 50*time.Millisecond, "a reaped group must not linger as zombies")
}
