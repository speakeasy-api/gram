package chat

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func chatMessageTarget(m *repo.ChatMessage) transcriptstore.OpenTarget {
	return transcriptstore.OpenTarget{MessageID: m.ID, Set: func(f transcriptcrypto.ProtectedFields) {
		*m = transcriptstore.WithProtectedFields(*m, f)
	}}
}

func riskWindowedTarget(m *repo.ListRiskWindowedMessagesRow) transcriptstore.OpenTarget {
	return transcriptstore.OpenTarget{MessageID: m.ID, Set: func(f transcriptcrypto.ProtectedFields) {
		m.Content, m.ContentRaw, m.ToolCalls = f.Content, f.ContentRaw, f.ToolCalls
	}}
}

func searchWindowedTarget(m *repo.ListSearchWindowedMessagesRow) transcriptstore.OpenTarget {
	return transcriptstore.OpenTarget{MessageID: m.ID, Set: func(f transcriptcrypto.ProtectedFields) {
		m.Content, m.ContentRaw, m.ToolCalls = f.Content, f.ContentRaw, f.ToolCalls
	}}
}

// openTranscriptRows opens a page of chat.load rows through the caller's project.
// See transcriptstore.Transcripts.Open for the fallback and failure rules.
func openTranscriptRows[T any](ctx context.Context, s *Service, authCtx *contextvalues.AuthContext, rows []T, targetOf func(*T) transcriptstore.OpenTarget) error {
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil
	}
	//nolint:wrapcheck // Open already wraps with the transcript read context.
	return transcriptstore.OpenChatMessages(ctx, s.transcripts, authCtx.ActiveOrganizationID, *authCtx.ProjectID, rows, targetOf)
}
