//go:build !windows

package relay

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"
)

// The project config is repository-controlled and can be a FIFO with no
// writer. Reading it must return rather than block: the read runs before the
// tool call's verdict, where a stall becomes an allow once the shim's deadline
// passes. Without the non-blocking open this never completes.
func TestPiMCPConfigDoesNotBlockOnFIFO(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".pi"), 0o755))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, ".pi", "mcp.json"), 0o600))

	done := make(chan []agenthooks.MCPServer, 1)
	go func() { done <- loadPiMCPServers(dir) }()

	select {
	case servers := <-done:
		require.Empty(t, servers)
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO config blocked the tool-call gating path")
	}
}
