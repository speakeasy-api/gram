package transcriptstore

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
)

// Readers for the Platform MCP chat tools.

// platformBodyBytes is the largest body field the Platform MCP chat tools
// return. A larger field is blanked and the row is marked oversized, in SQL
// for plaintext and by platformShape for opened shadows.
const platformBodyBytes = 131072

func platformShape(m *transcriptcrypto.Message) {
	if len(m.Content) > platformBodyBytes {
		m.Content = ""
	}
	if len(m.ToolCalls) > platformBodyBytes {
		m.ToolCalls = nil
	}
	if len(m.ContentRaw) > platformBodyBytes {
		m.ContentRaw = nil
	}
}

// SearchPlatformChatMessagesParams filters a project's messages for the
// Platform MCP chat search. OpenShadows is set by the method; leave it false.
type SearchPlatformChatMessagesParams = plainrepo.SearchPlatformChatMessagesParams

// SearchPlatformChatMessagesRow is one matching message.
type SearchPlatformChatMessagesRow = plainrepo.SearchPlatformChatMessagesRow

// SearchPlatformChatMessages returns one page of matching messages.
func (t *Transcripts) SearchPlatformChatMessages(ctx context.Context, db DBTX, params SearchPlatformChatMessagesParams) ([]SearchPlatformChatMessagesRow, error) {
	params.OpenShadows = t.Enabled(params.ProjectID)
	rows, err := plainrepo.New(db).SearchPlatformChatMessages(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("search platform chat messages: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: Body{MessageID: r.ID, Content: &r.Content, ContentRaw: &r.ContentRaw, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: platformShape}
	}
	if err := t.openJoined(ctx, db, params.ProjectID, joined); err != nil {
		return nil, err
	}
	return rows, nil
}

// ReadPlatformChatMessagesParams selects a window of one chat for the
// Platform MCP chat read. OpenShadows is set by the method; leave it false.
type ReadPlatformChatMessagesParams = plainrepo.ReadPlatformChatMessagesParams

// ReadPlatformChatMessagesRow is one message of the window.
type ReadPlatformChatMessagesRow = plainrepo.ReadPlatformChatMessagesRow

// ReadPlatformChatMessages returns a window of one chat, oldest first.
func (t *Transcripts) ReadPlatformChatMessages(ctx context.Context, db DBTX, params ReadPlatformChatMessagesParams) ([]ReadPlatformChatMessagesRow, error) {
	params.OpenShadows = t.Enabled(params.ProjectID)
	rows, err := plainrepo.New(db).ReadPlatformChatMessages(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("read platform chat messages: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: Body{MessageID: r.ID, Content: &r.Content, ContentRaw: &r.ContentRaw, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: platformShape}
	}
	if err := t.openJoined(ctx, db, params.ProjectID, joined); err != nil {
		return nil, err
	}
	return rows, nil
}

// ListOwnedChatTranscriptMessagesForRecallParams selects a caller-owned chat
// for session recall. OpenShadows is set by the method; leave it false.
type ListOwnedChatTranscriptMessagesForRecallParams = plainrepo.ListOwnedChatTranscriptMessagesForRecallParams

// ListOwnedChatTranscriptMessagesForRecallRow is one recalled message.
type ListOwnedChatTranscriptMessagesForRecallRow = plainrepo.ListOwnedChatTranscriptMessagesForRecallRow

// ListOwnedChatTranscriptMessagesForRecall returns the newest messages of a
// caller-owned chat's latest generation.
func (t *Transcripts) ListOwnedChatTranscriptMessagesForRecall(ctx context.Context, db DBTX, params ListOwnedChatTranscriptMessagesForRecallParams) ([]ListOwnedChatTranscriptMessagesForRecallRow, error) {
	projectID := params.ProjectID.UUID
	params.OpenShadows = params.ProjectID.Valid && t.Enabled(projectID)
	rows, err := plainrepo.New(db).ListOwnedChatTranscriptMessagesForRecall(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list owned chat transcript messages for recall: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: Body{MessageID: r.ID, Content: &r.Content, ContentRaw: nil, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: params.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if !params.OpenShadows {
		return rows, nil
	}
	if err := t.openJoined(ctx, db, projectID, joined); err != nil {
		return nil, err
	}
	return rows, nil
}
