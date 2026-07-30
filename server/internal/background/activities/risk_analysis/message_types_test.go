package risk_analysis

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
)

func TestFilterBatchMessagesByMessageTypes(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	assistantID := uuid.New()
	toolRequestID := uuid.New()
	toolResponseID := uuid.New()
	promptAttachmentID := uuid.New()

	messages := []batchMessage{
		{ID: userID, Type: message.User},
		{ID: assistantID, Type: message.Assistant},
		{ID: toolRequestID, Type: message.ToolRequest},
		{ID: toolResponseID, Type: message.ToolResponse},
		{ID: promptAttachmentID, Type: message.PromptAttachment, ContentPart: true},
	}

	filtered := filterBatchMessagesByMessageTypes(messages, []string{message.ToolRequest, message.ToolResponse, message.PromptAttachment})
	require.Len(t, filtered, 3)
	require.Equal(t, toolRequestID, filtered[0].ID)
	require.Equal(t, toolResponseID, filtered[1].ID)
	require.Equal(t, promptAttachmentID, filtered[2].ID)

	all := filterBatchMessagesByMessageTypes(messages, nil)
	require.Len(t, all, 5)
	require.Equal(t, []uuid.UUID{userID, assistantID, toolRequestID, toolResponseID, promptAttachmentID}, []uuid.UUID{all[0].ID, all[1].ID, all[2].ID, all[3].ID, all[4].ID})
}

func TestParseRecordedToolCallsMalformedFallback(t *testing.T) {
	t.Parallel()

	calls := parseRecordedToolCalls(context.Background(), slog.New(slog.DiscardHandler), []byte(`rm -rf /tmp/x`)) //nolint:forbidigo // same-package test import-cycles with testenv

	require.Len(t, calls, 1)
	require.Equal(t, malformedToolCallsName, calls[0].Function.Name)
	require.Equal(t, `rm -rf /tmp/x`, calls[0].Function.Arguments)
}
