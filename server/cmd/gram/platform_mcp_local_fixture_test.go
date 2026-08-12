package gram

import "testing"

// Production startup does not compose the local fixture. Synthetic provider
// setup remains covered in server/internal/platformmcp/localfixture tests.
func TestPlatformMCPStartupDoesNotExposeFixtureFlag(t *testing.T) {
	t.Parallel()

	const removedFlag = "platform-mcp-local-fixture"

	command := newStartCommand()
	for _, flag := range command.Flags {
		for _, name := range flag.Names() {
			if name == removedFlag {
				t.Fatalf("unexpected Platform MCP fixture flag %q", removedFlag)
			}
		}
	}
}
