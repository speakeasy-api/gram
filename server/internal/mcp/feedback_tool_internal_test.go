package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
)

// A feedback tool may not take a name the gateway answers itself in dynamic
// mode, so the feedback package's list must name every one of them.
func TestFeedbackToolCannotTakeTheGatewaysOwnToolNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{searchToolsToolName, listToolsToolName, describeToolsToolName, executeToolToolName} {
		require.Contains(t, mcpfeedback.ReservedToolNames, name)
	}
}
