package relay

import (
	"io"
	"os"
	"strings"
)

// maxMCPConfigBytes bounds one provider MCP config read. The file is read on
// the tool-call gating path, so it must never stall or balloon the relay.
const maxMCPConfigBytes = 1 << 20

// readMCPConfigBounded reads one provider MCP config file bounded and
// non-blocking, shared by every provider whose MCP discovery reads a
// repository-controlled file (Pi's mcp.json, Vibe's plugin mcp.json). A FIFO
// or a symlink to a device would block the read past the hook's deadline,
// turning the tool call's verdict into an allow. The open cannot block (see
// mcpNonBlockingOpenFlag) and the descriptor itself is checked, so a path
// swapped after an earlier stat cannot smuggle one in.
func readMCPConfigBounded(path string) []byte {
	f, err := os.OpenFile(path, os.O_RDONLY|mcpNonBlockingOpenFlag, 0)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxMCPConfigBytes {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxMCPConfigBytes+1))
	if err != nil || len(data) > maxMCPConfigBytes {
		return nil
	}
	return data
}

// joinMCPServerCommand joins a stdio server's command and args the same way
// every provider presents them, so servers launched by the same executable
// keep their distinguishing provenance.
func joinMCPServerCommand(command string, args []string) string {
	command = strings.TrimSpace(command)
	if command != "" && len(args) > 0 {
		return strings.Join(append([]string{command}, args...), " ")
	}
	return command
}
