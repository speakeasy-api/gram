//go:build !windows

package relay

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A project's .claude/settings.json is repository-controlled and can be a FIFO
// with no writer. Reading it must return rather than block the tool-call gating path.
func TestReadClaudePluginFileDoesNotBlockOnFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, syscall.Mkfifo(path, 0o600))

	done := make(chan bool, 1)
	go func() {
		_, ok := readClaudePluginFile(path)
		done <- ok
	}()

	select {
	case ok := <-done:
		require.False(t, ok)
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO config blocked the tool-call gating path")
	}
}
