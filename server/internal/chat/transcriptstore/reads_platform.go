package transcriptstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
)

// Readers for the Platform MCP chat tools.

// The Platform MCP chat tools return no body field over 128 KiB. The query
// decides which fields are over the cap, from the shadow's metadata for a
// project that keeps shadows (jsonb text lengths, longer than the compact JSON
// a shadow holds), blanks them in the plaintext columns and flags each one.
// platformShape blanks the same fields of the opened shadow by those flags, so
// both sides agree on which field was too large.
type platformOversized struct {
	content, toolCalls, contentRaw bool
}

func platformShape(o platformOversized) func(*transcriptcrypto.Message) {
	return func(m *transcriptcrypto.Message) {
		if o.content {
			m.Content = ""
		}
		if o.toolCalls {
			m.ToolCalls = nil
		}
		if o.contentRaw {
			m.ContentRaw = nil
		}
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
		joined[i] = joinedRow{body: Body{MessageID: r.ID, Content: &r.Content, ContentRaw: &r.ContentRaw, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: platformShape(platformOversized{content: r.ContentOversized, toolCalls: r.ToolCallsOversized, contentRaw: r.ContentRawOversized})}
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
		joined[i] = joinedRow{body: Body{MessageID: r.ID, Content: &r.Content, ContentRaw: &r.ContentRaw, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: platformShape(platformOversized{content: r.ContentOversized, toolCalls: r.ToolCallsOversized, contentRaw: r.ContentRawOversized})}
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
	// A recall without a project reads no shadows: uuid.Nil is never listed.
	projectID := uuid.Nil
	if params.ProjectID.Valid {
		projectID = params.ProjectID.UUID
	}
	params.OpenShadows = t.Enabled(projectID)
	rows, err := plainrepo.New(db).ListOwnedChatTranscriptMessagesForRecall(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list owned chat transcript messages for recall: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: Body{MessageID: r.ID, Content: &r.Content, ContentRaw: nil, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: params.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if err := t.openJoined(ctx, db, projectID, joined); err != nil {
		return nil, err
	}
	return rows, nil
}
