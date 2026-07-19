package mcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseInstructionToolMode(t *testing.T) {
	t.Parallel()

	require.Equal(t, instructionToolModeDisabled, parseInstructionToolMode("disabled"))
	require.Equal(t, instructionToolModeOptional, parseInstructionToolMode("optional"))
	require.Equal(t, instructionToolModeRequired, parseInstructionToolMode("required"))
	// Legacy rows and unknown values fail safe to the default.
	require.Equal(t, instructionToolModeRequired, parseInstructionToolMode(""))
	require.Equal(t, instructionToolModeRequired, parseInstructionToolMode("bogus"))
}

func TestInjectInstructionTool(t *testing.T) {
	t.Parallel()

	entries := []*toolListEntry{{Name: "create_ticket"}, {Name: "list_tickets"}}

	t.Run("prepends the tool first", func(t *testing.T) {
		t.Parallel()
		out := injectInstructionTool(entries, nil, instructionToolModeRequired)
		require.Len(t, out, 3)
		require.Equal(t, instructionsToolName, out[0].Name)
		require.Equal(t, "create_ticket", out[1].Name)
	})

	t.Run("disabled mode injects nothing", func(t *testing.T) {
		t.Parallel()
		out := injectInstructionTool(entries, nil, instructionToolModeDisabled)
		require.Len(t, out, 2)
	})

	t.Run("skips on name collision with a listed tool", func(t *testing.T) {
		t.Parallel()
		colliding := append([]*toolListEntry{{Name: instructionsToolName}}, entries...)
		out := injectInstructionTool(colliding, nil, instructionToolModeRequired)
		require.Len(t, out, 3)
	})

	t.Run("optional mode still injects", func(t *testing.T) {
		t.Parallel()
		out := injectInstructionTool(entries, nil, instructionToolModeOptional)
		require.Equal(t, instructionsToolName, out[0].Name)
	})
}

func TestInstructionSessionGateCacheKey(t *testing.T) {
	t.Parallel()

	g := instructionSessionGate{ToolsetID: "ts-1", SessionID: "sess-9"}
	require.Equal(t, "mcpInstructionsRead:ts-1:sess-9", g.CacheKey())
	require.Equal(t, g.CacheKey(), instructionGateCacheKey("ts-1", "sess-9"))
	require.Empty(t, g.AdditionalCacheKeys())
	require.Equal(t, 60*time.Minute, g.TTL())
}
