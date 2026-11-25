package transcriptstore

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
)

// Readers for signal evaluation.

// LoadEvaluationMessagesParams selects persisted messages to evaluate.
// OpenShadows is set by the method; leave it false.
type LoadEvaluationMessagesParams = plainrepo.LoadEvaluationMessagesParams

// LoadEvaluationMessagesRow is one message to evaluate.
type LoadEvaluationMessagesRow = plainrepo.LoadEvaluationMessagesRow

// LoadEvaluationMessages reads non-system messages and their attachment
// locators.
func (t *Transcripts) LoadEvaluationMessages(ctx context.Context, db DBTX, params LoadEvaluationMessagesParams) ([]LoadEvaluationMessagesRow, error) {
	params.OpenShadows = t.Enabled(params.ProjectID)
	rows, err := plainrepo.New(db).LoadEvaluationMessages(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("load evaluation messages: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: Body{MessageID: r.ID, Content: &r.Content, ContentRaw: &r.ContentRaw, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if err := t.openJoined(ctx, db, params.ProjectID, joined); err != nil {
		return nil, err
	}
	return rows, nil
}

// LoadEvaluationHistoryParams selects the history before an evaluated
// message. OpenShadows is set by the method; leave it false.
type LoadEvaluationHistoryParams = plainrepo.LoadEvaluationHistoryParams

// LoadEvaluationHistoryRow is one history message; bodies past the content
// budget are blanked and marked ContentTooLarge. For a project that keeps
// shadows the budget takes each body size from the shadow's metadata and falls
// back to measuring the plaintext column only for a message with no shadow or
// with NULL metadata. The opened shadow is blanked by the same flag.
type LoadEvaluationHistoryRow = plainrepo.LoadEvaluationHistoryRow

// LoadEvaluationHistory reads the history before an evaluated message,
// newest first.
func (t *Transcripts) LoadEvaluationHistory(ctx context.Context, db DBTX, params LoadEvaluationHistoryParams) ([]LoadEvaluationHistoryRow, error) {
	params.OpenShadows = t.Enabled(params.ProjectID)
	rows, err := plainrepo.New(db).LoadEvaluationHistory(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("load evaluation history: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		var shape func(*transcriptcrypto.Message)
		if r.ContentTooLarge {
			shape = blankBody
		}
		joined[i] = joinedRow{body: Body{MessageID: r.ID, Content: &r.Content, ContentRaw: &r.ContentRaw, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: params.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: shape}
	}
	if err := t.openJoined(ctx, db, params.ProjectID, joined); err != nil {
		return nil, err
	}
	return rows, nil
}

// blankBody matches a body the query blanked for being over budget.
func blankBody(m *transcriptcrypto.Message) {
	m.Content = ""
	m.ContentRaw = nil
	m.ToolCalls = nil
}
