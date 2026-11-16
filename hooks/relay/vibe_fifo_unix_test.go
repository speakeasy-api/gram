//go:build !windows

package relay

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"
)

// TestLoadVibeMCPServersSkipsUnsafeFiles: a FIFO or an oversized mcp.json in
// the project scope must be skipped, not stall or balloon the gating read. The
// FIFO case runs the load in a goroutine with a deadline, so a regression of
// the non-blocking open fails here instead of hanging until the global go
// test timeout (same shape as TestPiMCPConfigDoesNotBlockOnFIFO).
func TestLoadVibeMCPServersSkipsUnsafeFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("VIBE_HOME", home)
	cwd := t.TempDir()
	projDir := filepath.Join(cwd, ".vibe", "plugins", "evil")
	require.NoError(t, os.MkdirAll(projDir, 0o755))
	require.NoError(t, syscall.Mkfifo(filepath.Join(projDir, "mcp.json"), 0o600))
	bigDir := filepath.Join(cwd, ".vibe", "plugins", "big")
	require.NoError(t, os.MkdirAll(bigDir, 0o755))
	// A valid mcpServers document padded with spaces past the size bound, so
	// only the size check (not a JSON parse failure) causes the skip.
	big := `{"mcpServers":{"big":{"url":"https://big.example.test/mcp"}}}` + strings.Repeat(" ", maxMCPConfigBytes)
	require.NoError(t, os.WriteFile(filepath.Join(bigDir, "mcp.json"), []byte(big), 0o600))

	done := make(chan []agenthooks.MCPServer, 1)
	go func() { done <- loadVibeMCPServers(cwd) }()
	select {
	case servers := <-done:
		require.Empty(t, servers)
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO config blocked the tool-call gating path")
	}
}
