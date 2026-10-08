//go:build unix

package agent

import (
	"errors"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStdioBridgeStopsServerProcessGroup(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	require.NoError(t, err)
	srv, a := newStdioTestServerWithCommand(t, "sleep 300 & "+stdioFixtureEnv+"=1 '"+exe+"'", 0)
	sid := initializeSession(t, srv)

	sess := a.stdio.session(sid)
	require.NotNil(t, sess)
	pgid := sess.cmd.Process.Pid

	require.Equal(t, http.StatusNoContent, mcpRequest(t, srv, http.MethodDelete, sid, "").StatusCode)
	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
	}, 20*time.Second, 100*time.Millisecond, "background children of the server must be stopped too")
}

func TestStdioBridgeCloseWaitsForTermIgnoringDescendants(t *testing.T) {
	t.Parallel()
	// The shell exits as soon as stdin closes, leaving a child that ignores
	// SIGTERM; only SIGKILL stops it.
	_, a := newStdioTestServerWithCommand(t, "(trap '' TERM; exec sleep 300) & read ignored", 0)
	sess, err := a.stdio.start()
	require.NoError(t, err)
	pgid := sess.cmd.Process.Pid

	a.stdio.Close()
	require.ErrorIs(t, syscall.Kill(-pgid, 0), syscall.ESRCH, "Close must not return while the server's process group survives")
}
