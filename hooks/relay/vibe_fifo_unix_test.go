//go:build !windows

package relay

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLoadVibeMCPServersSkipsUnsafeFiles: a FIFO or an oversized mcp.json in
// the project scope must be skipped, not stall or balloon the gating read.
func TestLoadVibeMCPServersSkipsUnsafeFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("VIBE_HOME", home)
	cwd := t.TempDir()
	projDir := filepath.Join(cwd, ".vibe", "plugins", "evil")
	require.NoError(t, os.MkdirAll(projDir, 0o755))
	require.NoError(t, syscall.Mkfifo(filepath.Join(projDir, "mcp.json"), 0o600))
	bigDir := filepath.Join(cwd, ".vibe", "plugins", "big")
	require.NoError(t, os.MkdirAll(bigDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bigDir, "mcp.json"), make([]byte, maxMCPConfigBytes+1), 0o600))

	require.Empty(t, loadVibeMCPServers(cwd))
}
