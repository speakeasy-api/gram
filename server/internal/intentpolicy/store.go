package intentpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// SourceUpload marks messages of a transcript uploaded on the analysis page.
	SourceUpload = "intent-policies-upload"

	// SourceImport marks messages written by the local eval-set import.
	SourceImport = "intent-policies-import"

	// MaxTranscriptBytes caps one transcript. The largest eval-set transcripts
	// are a few MB; 20 MB leaves room for long real sessions.
	MaxTranscriptBytes = 20 << 20 // 20 MiB
)

// StoreParams identifies where a parsed transcript is written.
type StoreParams struct {
	// ChatID is the chat to create; uuid.Nil generates one.
	ChatID uuid.UUID

	// ProjectID owns the chat.
	ProjectID uuid.UUID

	// OrganizationID owns the project.
	OrganizationID string

	// UserID is the Speakeasy user recorded as the chat author.
	UserID string

	// Title is the chat title.
	Title string

	// Source is stamped on every message.
	Source string
}

type storedCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type storedCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function storedCallFunction `json:"function"`
}

// StoreTranscript writes the transcript as a new chat through the standard
// chat message writer and returns its id.
func StoreTranscript(ctx context.Context, writer *chat.ChatMessageWriter, p StoreParams, t Transcript, now time.Time) (uuid.UUID, error) {
	chatID := p.ChatID
	if chatID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, fmt.Errorf("generate chat id: %w", err)
		}
		chatID = id
	}

	writes := make([]chat.MessageWrite, 0, len(t.Messages))
	prev := now
	for i, m := range t.Messages {
		at := now.Add(time.Duration(i) * time.Millisecond)
		if m.Timestamp != nil {
			at = *m.Timestamp
		}
		if i > 0 && !at.After(prev) {
			at = prev.Add(time.Millisecond)
		}
		prev = at

		var toolCalls []byte
		if len(m.ToolCalls) > 0 {
			calls := make([]storedCall, 0, len(m.ToolCalls))
			for _, c := range m.ToolCalls {
				calls = append(calls, storedCall{ID: stripNUL(c.ID), Type: "function", Function: storedCallFunction{Name: stripNUL(c.Name), Arguments: stripJSONNUL(string(c.Arguments))}})
			}
			encoded, err := json.Marshal(calls)
			if err != nil {
				return uuid.Nil, fmt.Errorf("encode tool calls: %w", err)
			}
			toolCalls = encoded
		}

		writes = append(writes, chat.MessageWrite{
			Params: chatrepo.CreateChatMessageParams{
				ID:               uuid.Nil,
				ChatID:           chatID,
				Role:             m.Role,
				ProjectID:        p.ProjectID,
				Content:          stripNUL(m.Content),
				ContentRaw:       nil,
				ContentAssetUrl:  pgtype.Text{String: "", Valid: false},
				StorageError:     pgtype.Text{String: "", Valid: false},
				Model:            pgtype.Text{String: "", Valid: false},
				MessageID:        pgtype.Text{String: "", Valid: false},
				ToolCallID:       conv.ToPGTextEmpty(stripNUL(m.ToolCallID)),
				UserID:           conv.ToPGTextEmpty(p.UserID),
				ExternalUserID:   pgtype.Text{String: "", Valid: false},
				FinishReason:     pgtype.Text{String: "", Valid: false},
				ToolCalls:        toolCalls,
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
				Origin:           pgtype.Text{String: "", Valid: false},
				UserAgent:        pgtype.Text{String: "", Valid: false},
				IpAddress:        pgtype.Text{String: "", Valid: false},
				Source:           conv.ToPGText(p.Source),
				ContentHash:      nil,
				Generation:       0,
				Replayed:         false,
				CreatedAt:        conv.ToPGTimestamptz(at),
			},
			BillingUserID:     p.UserID,
			AssistantID:       uuid.Nil,
			WorkloadSource:    metering.WorkloadSourceImport,
			UserEmail:         "",
			Provider:          "",
			InferenceProvider: "",
			Lineage:           nil,
			HookHostname:      "",
			AccountType:       "",
			BillingMode:       "",
		})
	}

	// One transaction, so a failed message write leaves no empty chat behind.
	tx, err := writer.Begin(ctx, p.ProjectID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin transcript transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	if _, err := hooksrepo.New(tx).UpsertClaudeCodeSession(ctx, hooksrepo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      p.ProjectID,
		OrganizationID: p.OrganizationID,
		UserID:         conv.ToPGTextEmpty(p.UserID),
		ExternalUserID: pgtype.Text{String: "", Valid: false},
		UserAccountID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Title:          conv.ToPGText(stripNUL(p.Title)),
		Cwd:            conv.ToPGTextEmpty(stripNUL(t.Cwd)),
	}); err != nil {
		return uuid.Nil, fmt.Errorf("create chat: %w", err)
	}
	if _, err := writer.WriteInTx(ctx, tx, writes); err != nil {
		return uuid.Nil, fmt.Errorf("write transcript messages: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit transcript: %w", err)
	}
	writer.NotifyStoredRows(ctx, p.ProjectID, writes)
	return chatID, nil
}

// stripNUL removes NUL characters, which Postgres text and jsonb columns
// reject. Captured tool output sometimes contains them.
func stripNUL(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

// stripJSONNUL removes raw NULs and \u0000 escapes from JSON text, leaving an
// escaped backslash followed by "u0000" intact.
func stripJSONNUL(s string) string {
	s = stripNUL(s)
	if !strings.Contains(s, `\u0000`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		if s[i+1] == 'u' && strings.HasPrefix(s[i:], `\u0000`) {
			i += len(`\u0000`) - 1
			continue
		}
		b.WriteString(s[i : i+2])
		i++
	}
	return b.String()
}
