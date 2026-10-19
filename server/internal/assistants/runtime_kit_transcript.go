package assistants

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/metering"
)

// KitTranscriptBinding must come from persisted session admission, not ACP input.
// UserEchoes maps accepted ACP user-message IDs to existing dashboard message rows.
// Supply only authoritative correlations; never deduplicate by text equality.
type KitTranscriptBinding struct {
	RuntimeID, ProjectID, AssistantID, ThreadID, ChatID uuid.UUID
	SessionID                                           string
	UserEchoes                                          map[string]uuid.UUID
}

// KitTranscriptResult describes projection, not runtime settlement. Source
// adapters can replace text by MessageID (Replacement), or append transient
// chunks. ForegroundState == idle does not mean background work has settled.
type KitTranscriptResult struct {
	Role                string
	StopReason          string
	MessageID           string
	SourceText          string
	Replacement         bool
	Changed             bool
	Transient           bool
	Ignored             bool
	ForegroundState     string
	GenericContentTypes []string
}

type KitTranscriptProjector struct {
	db              *pgxpool.Pool
	writer          *chat.ChatMessageWriter
	stream          *chat.TurnStream
	validateSession func(context.Context, pgx.Tx, KitTranscriptBinding) error
}

// NewKitTranscriptProjector requires a persisted-session validator. It runs under
// the same assistant transaction advisory lock as runtime admission and switching.
func NewKitTranscriptProjector(db *pgxpool.Pool, writer *chat.ChatMessageWriter, stream *chat.TurnStream,
	validateSession func(context.Context, pgx.Tx, KitTranscriptBinding) error,
) *KitTranscriptProjector {
	return &KitTranscriptProjector{db: db, writer: writer, stream: stream, validateSession: validateSession}
}

func (p *KitTranscriptProjector) guard(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
	if p.validateSession == nil || b.SessionID == "" || b.RuntimeID == uuid.Nil || b.AssistantID == uuid.Nil || b.ProjectID == uuid.Nil || b.ThreadID == uuid.Nil || b.ChatID == uuid.Nil {
		return fmt.Errorf("kit transcript requires persisted session binding")
	}
	if err := assistantrepo.New(tx).AcquireAssistantAdvisoryLock(ctx, b.AssistantID.String()); err != nil {
		return fmt.Errorf("acquire assistant transcript lock: %w", err)
	}
	_, err := chatrepo.New(tx).ValidateKitTranscriptBinding(ctx, chatrepo.ValidateKitTranscriptBindingParams{ProjectID: b.ProjectID, AssistantID: b.AssistantID, ThreadID: b.ThreadID, ChatID: b.ChatID, RuntimeID: b.RuntimeID})
	if err != nil {
		return fmt.Errorf("stale Kit transcript runtime: %w", err)
	}
	return p.validateSession(ctx, tx, b)
}

// KitTranscriptMessageID separates runtime generations and sessions; replays and
// replacement updates for one ACP message always address the same canonical row.
func KitTranscriptMessageID(b KitTranscriptBinding, kind, messageID string) uuid.UUID {
	data, _ := json.Marshal([]string{b.SessionID, kind, messageID})
	return uuid.NewSHA1(b.RuntimeID, data)
}

type kitTranscriptUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	MessageID     string          `json:"messageId"`
	ToolCallID    string          `json:"toolCallId"`
	Content       json.RawMessage `json:"content"`
	State         string          `json:"state"`
	StopReason    string          `json:"stopReason"`
}

func (p *KitTranscriptProjector) Handle(ctx context.Context, b KitTranscriptBinding, n kitacp.Notification) (KitTranscriptResult, error) {
	var envelope struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if n.Method != "session/update" {
		var out KitTranscriptResult
		out.Ignored = true
		return out, nil
	}
	if err := json.Unmarshal(n.Params, &envelope); err != nil {
		return KitTranscriptResult{}, fmt.Errorf("decode kit transcript update: %w", err)
	}
	if envelope.SessionID != b.SessionID || (n.SessionID != "" && n.SessionID != b.SessionID) {
		return KitTranscriptResult{}, fmt.Errorf("kit transcript session mismatch")
	}
	var u kitTranscriptUpdate
	if err := json.Unmarshal(envelope.Update, &u); err != nil {
		return KitTranscriptResult{}, fmt.Errorf("decode kit transcript update: %w", err)
	}
	var out KitTranscriptResult
	out.MessageID = u.MessageID
	if p.writer == nil {
		return out, fmt.Errorf("kit transcript writer unavailable")
	}
	if u.SessionUpdate == "tool_call_update" {
		return p.handleKitToolUpdate(ctx, b, u, envelope.Update)
	}
	switch u.SessionUpdate {
	case "user_message", "agent_message":
		key := u.MessageID
		if key == "" {
			return out, fmt.Errorf("kit transcript update missing stable ID")
		}
		out.MessageID = key
		out.Role = "assistant"
		if u.SessionUpdate == "user_message" {
			out.Role = "user"
		}
		if echo, ok := b.UserEchoes[key]; ok && u.SessionUpdate == "user_message" {
			err := p.guarded(ctx, b, func(tx pgx.Tx) error {
				row, err := chatrepo.New(tx).GetKitProjectionMessage(ctx, chatrepo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: b.ProjectID, Valid: true}, ID: echo})
				if err != nil {
					return fmt.Errorf("get correlated user message: %w", err)
				}
				if row.ChatID != b.ChatID || row.Role != "user" {
					return fmt.Errorf("invalid Kit user echo correlation")
				}
				return nil
			})
			out.Ignored = true
			return out, err
		}
		id := KitTranscriptMessageID(b, u.SessionUpdate, key)
		changed, err := p.writer.ProjectMessage(ctx, b.ProjectID, id, func(ctx context.Context, tx pgx.Tx) error { return p.guard(ctx, tx, b) }, func(old *chatrepo.ChatMessage) (chat.MessageWrite, bool, error) {
			write, apply, text, generic, err := translateKitMessage(b, id, u, envelope.Update, old)
			out.SourceText, out.GenericContentTypes = text, generic
			return write, apply, err
		})
		out.Changed, out.Replacement = changed, true
		if err != nil {
			return out, fmt.Errorf("project kit transcript message: %w", err)
		}
		return out, nil
	case "agent_message_chunk":
		text, _, _, err := kitContent([]json.RawMessage{u.Content})
		if err != nil {
			return out, err
		}
		out.SourceText, out.Transient, out.Role = text, true, "assistant"
		err = p.guarded(ctx, b, func(_ pgx.Tx) error {
			var frame chat.TurnFrame
			frame.Kind, frame.Text = chat.TurnFrameText, text
			_, _ = p.stream.Publish(ctx, b.ChatID, frame)
			return nil
		})
		return out, err
	case "state_update":
		out.ForegroundState, out.StopReason = u.State, u.StopReason
		// This marks only the dashboard foreground turn. Never use it as a snapshot fence.
		err := p.guarded(ctx, b, func(_ pgx.Tx) error {
			if u.State == "idle" {
				var frame chat.TurnFrame
				frame.Kind = chat.TurnFrameDone
				_, _ = p.stream.Publish(ctx, b.ChatID, frame)
			}
			return nil
		})
		return out, err
	default:
		out.Ignored = true
		return out, p.guarded(ctx, b, func(pgx.Tx) error { return nil })
	}
}

func (p *KitTranscriptProjector) guarded(ctx context.Context, b KitTranscriptBinding, fn func(pgx.Tx) error) error {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transcript transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := p.guard(ctx, tx, b); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transcript transaction: %w", err)
	}
	return nil
}

func translateKitMessage(b KitTranscriptBinding, id uuid.UUID, u kitTranscriptUpdate, raw json.RawMessage, old *chatrepo.ChatMessage) (chat.MessageWrite, bool, string, []string, error) {
	role := "assistant"
	if u.SessionUpdate == "user_message" {
		role = "user"
	}
	if u.SessionUpdate == "tool_call_update" {
		role = "tool"
	}
	var write chat.MessageWrite
	write.Params.ID, write.Params.ProjectID, write.Params.ChatID = id, b.ProjectID, b.ChatID
	write.Params.Role = role
	write.Params.Source = pgtype.Text{String: "kit", Valid: true}
	write.Params.MessageID = pgtype.Text{String: id.String(), Valid: true}
	write.AssistantID, write.WorkloadSource = b.AssistantID, metering.WorkloadSourceAssistant
	var text string
	var content []byte
	var generic []string
	if role == "tool" {
		// Tool patches are sparse objects. Retain unknown/diff/terminal/raw-output
		// payloads losslessly as canonical text JSON rather than pretending they are images.
		merged := map[string]json.RawMessage{}
		if old != nil {
			if err := json.Unmarshal([]byte(old.Content), &merged); err != nil {
				return write, false, "", nil, fmt.Errorf("invalid stored Kit tool patch: %w", err)
			}
		}
		patch := map[string]json.RawMessage{}
		if err := json.Unmarshal(raw, &patch); err != nil {
			return write, false, "", nil, fmt.Errorf("translate kit transcript content: %w", err)
		}
		for key, value := range patch {
			merged[key] = value
		}
		encoded, err := json.Marshal(merged)
		if err != nil {
			return write, false, "", nil, fmt.Errorf("translate kit transcript content: %w", err)
		}
		text = string(encoded)
		content, _ = json.Marshal([]map[string]string{{"type": "text", "text": text}})
		write.Params.ToolCallID = pgtype.Text{String: u.ToolCallID, Valid: true}
	} else {
		if len(u.Content) == 0 && old != nil {
			return write, false, "", nil, nil
		}
		var blocks []json.RawMessage
		if len(u.Content) != 0 && string(u.Content) != "null" {
			if err := json.Unmarshal(u.Content, &blocks); err != nil {
				return write, false, "", nil, fmt.Errorf("translate kit transcript content: %w", err)
			}
		}
		var err error
		text, content, generic, err = kitContent(blocks)
		if err != nil {
			return write, false, "", nil, fmt.Errorf("translate kit transcript content: %w", err)
		}
	}
	write.Params.Content, write.Params.ContentRaw = text, content
	digest := sha256.Sum256(content)
	write.Params.ContentHash = digest[:]
	if old != nil {
		write.Params.Generation = old.Generation
		write.Params.CreatedAt = old.CreatedAt
	}
	return write, true, text, generic, nil
}

// kitContent keeps canonical text parts. Nontext ACP blocks are retained as
// JSON text, not silently dropped or mislabeled as supported rich media.
func kitContent(blocks []json.RawMessage) (string, []byte, []string, error) {
	parts := make([]map[string]string, 0, len(blocks))
	texts := make([]string, 0, len(blocks))
	var generic []string
	for _, raw := range blocks {
		var block struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &block); err != nil {
			return "", nil, nil, fmt.Errorf("decode kit content block: %w", err)
		}
		if block.Type == "" {
			return "", nil, nil, fmt.Errorf("ACP content block missing type")
		}
		value := block.Text
		if block.Type != "text" {
			value = string(raw)
			generic = append(generic, block.Type)
		}
		parts = append(parts, map[string]string{"type": "text", "text": value})
		texts = append(texts, value)
	}
	encoded, err := json.Marshal(parts)
	return strings.Join(texts, "\n"), encoded, generic, err
}
