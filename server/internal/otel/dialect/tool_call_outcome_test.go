package dialect_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

func TestToolCallOutcomeFollowsTheIsErrorRule(t *testing.T) {
	t.Parallel()

	require.Equal(t, dialect.OutcomeOK, dialect.ToolCallOutcome(http.StatusOK, false))
	require.Equal(t, dialect.OutcomeOK, dialect.ToolCallOutcome(http.StatusNoContent, false))
	require.Equal(t, dialect.OutcomeError, dialect.ToolCallOutcome(http.StatusBadRequest, false))
	require.Equal(t, dialect.OutcomeError, dialect.ToolCallOutcome(http.StatusBadGateway, false))
	require.Equal(t, dialect.OutcomeError, dialect.ToolCallOutcome(http.StatusFound, false), "a redirect is not a tool result, so the client sees isError and so does the row")
	require.Equal(t, dialect.OutcomeError, dialect.ToolCallOutcome(http.StatusOK, true), "a passthrough result's own isError is the verdict the client reads")
}

func TestMCPResultIsErrorReadsOnlyAResultDocument(t *testing.T) {
	t.Parallel()

	require.True(t, dialect.MCPResultIsError([]byte(`{"content":[{"type":"text","text":"boom"}],"isError":true}`)))
	require.False(t, dialect.MCPResultIsError([]byte(`{"content":[{"type":"text","text":"ok"}]}`)))
	require.False(t, dialect.MCPResultIsError([]byte(`{"content":[],"isError":false}`)))
	require.False(t, dialect.MCPResultIsError([]byte(`not json`)))
	require.False(t, dialect.MCPResultIsError(nil))
}
