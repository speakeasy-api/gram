package chat

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func chatMessageBody(m *repo.ChatMessage) transcriptstore.Body {
	return transcriptstore.Body{MessageID: m.ID, Content: &m.Content, ContentRaw: &m.ContentRaw, ToolCalls: &m.ToolCalls, ToolOutcomeNotes: &m.ToolOutcomeNotes, ToolCallSummaries: &m.ToolCallSummaries, StorageError: &m.StorageError}
}

func riskWindowedMessageBody(m *repo.ListRiskWindowedMessagesRow) transcriptstore.Body {
	return transcriptstore.Body{MessageID: m.ID, Content: &m.Content, ContentRaw: &m.ContentRaw, ToolCalls: &m.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}
}

func searchWindowedMessageBody(m *repo.ListSearchWindowedMessagesRow) transcriptstore.Body {
	return transcriptstore.Body{MessageID: m.ID, Content: &m.Content, ContentRaw: &m.ContentRaw, ToolCalls: &m.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}
}

// openTranscriptRows opens a page of chat.load rows through the caller's project.
// See transcriptstore.Transcripts.Open for the fallback and failure rules.
func openTranscriptRows[T any](ctx context.Context, s *Service, authCtx *contextvalues.AuthContext, rows []T, body func(*T) transcriptstore.Body) error {
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil
	}
	//nolint:wrapcheck // Open already wraps with the transcript read context.
	return transcriptstore.OpenRows(ctx, s.transcripts, authCtx.ActiveOrganizationID, *authCtx.ProjectID, rows, body)
}
