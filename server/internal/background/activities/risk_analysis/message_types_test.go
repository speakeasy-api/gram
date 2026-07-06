package risk_analysis

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestFilterMessagesByMessageTypes(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	assistantID := uuid.New()
	toolRequestID := uuid.New()
	toolResponseID := uuid.New()

	messages := []repo.GetMessageContentBatchRow{
		{ID: userID, Role: "user", Content: "hello"},
		{ID: assistantID, Role: "assistant", Content: "thinking"},
		{ID: toolRequestID, Role: "assistant", Content: "", ToolCalls: []byte(`[]`)},
		{ID: toolResponseID, Role: "tool", Content: "done"},
		{ID: uuid.New(), Role: "system", Content: "ignore"},
	}

	filtered := filterMessagesByMessageTypes(messages, []string{message.ToolRequest, message.ToolResponse})
	require.Len(t, filtered, 2)
	require.Equal(t, toolRequestID, filtered[0].ID)
	require.Equal(t, toolResponseID, filtered[1].ID)

	all := filterMessagesByMessageTypes(messages, nil)
	require.Len(t, all, 4)
	require.Equal(t, []uuid.UUID{userID, assistantID, toolRequestID, toolResponseID}, []uuid.UUID{all[0].ID, all[1].ID, all[2].ID, all[3].ID})
}

func TestParseRecordedToolCallsMalformedFallback(t *testing.T) {
	t.Parallel()

	calls := parseRecordedToolCalls(context.Background(), slog.New(slog.DiscardHandler), []byte(`rm -rf /tmp/x`)) //nolint:forbidigo // same-package test import-cycles with testenv

	require.Len(t, calls, 1)
	require.Equal(t, malformedToolCallsName, calls[0].Function.Name)
	require.Equal(t, `rm -rf /tmp/x`, calls[0].Function.Arguments)
}
