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
