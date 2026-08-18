package activities

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/researchagent"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// A run that failed before any tool call has a nil trace; it must still store
// an empty array, because JSON null fails the report's array check and would
// sink the failure write.
func TestEncodeToolCallTrace_NilBecomesEmptyArray(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	require.Equal(t, []byte("[]"), encodeToolCallTrace(logger, t.Context(), nil))
	require.Equal(t, []byte("[]"), encodeToolCallTrace(logger, t.Context(), []researchagent.ToolCallRecord{}))

	encoded := encodeToolCallTrace(logger, t.Context(), []researchagent.ToolCallRecord{{Sequence: 0, Tool: "web_search"}})
	require.Contains(t, string(encoded), `"tool":"web_search"`)
}
