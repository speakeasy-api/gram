package intentpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/metering"
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
func StoreTranscript(ctx context.Context, db *pgxpool.Pool, writer *chat.ChatMessageWriter, p StoreParams, t Transcript, now time.Time) (uuid.UUID, error) {
	chatID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("generate chat id: %w", err)
	}

	if _, err := hooksrepo.New(db).UpsertClaudeCodeSession(ctx, hooksrepo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      p.ProjectID,
		OrganizationID: p.OrganizationID,
		UserID:         conv.ToPGText(p.UserID),
		ExternalUserID: pgtype.Text{String: "", Valid: false},
		UserAccountID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Title:          conv.ToPGText(p.Title),
		Cwd:            conv.ToPGTextEmpty(t.Cwd),
	}); err != nil {
		return uuid.Nil, fmt.Errorf("create chat: %w", err)
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
				calls = append(calls, storedCall{ID: c.ID, Type: "function", Function: storedCallFunction{Name: c.Name, Arguments: string(c.Arguments)}})
			}
			toolCalls, err = json.Marshal(calls)
			if err != nil {
				return uuid.Nil, fmt.Errorf("encode tool calls: %w", err)
			}
		}

		writes = append(writes, chat.MessageWrite{
			Params: chatrepo.CreateChatMessageParams{
				ID:               uuid.Nil,
				ChatID:           chatID,
				Role:             m.Role,
				ProjectID:        p.ProjectID,
				Content:          m.Content,
				ContentRaw:       nil,
				ContentAssetUrl:  pgtype.Text{String: "", Valid: false},
				StorageError:     pgtype.Text{String: "", Valid: false},
				Model:            pgtype.Text{String: "", Valid: false},
				MessageID:        pgtype.Text{String: "", Valid: false},
				ToolCallID:       conv.ToPGTextEmpty(m.ToolCallID),
				UserID:           conv.ToPGText(p.UserID),
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

	if _, err := writer.Write(ctx, p.ProjectID, writes); err != nil {
		return uuid.Nil, fmt.Errorf("write transcript messages: %w", err)
	}
	return chatID, nil
}
