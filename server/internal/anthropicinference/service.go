package anthropicinference

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type scanner interface {
	ScanForEnforcement(context.Context, string, uuid.UUID, string, string, message.Type, string) (*risk.ScanResult, error)
}

type transcriptStore interface {
	ResolveActor(context.Context, Config, Actor) (string, error)
	Save(context.Context, Config, Frame, string) error
}

// Service archives transcripts and runs the existing risk-policy scanner.
type Service struct {
	store   transcriptStore
	scanner scanner
}

// NewService uses the shared chat writer so captured messages receive the same
// storage, metering, and asynchronous analysis as other imported conversations.
func NewService(db *pgxpool.Pool, writer *chat.ChatMessageWriter, scanner scanner) *Service {
	return &Service{store: &postgresStore{db: db, writer: writer}, scanner: scanner}
}

// Process stores the supplied history and returns a verdict for all known content.
func (s *Service) Process(ctx context.Context, config Config, frame Frame) (Verdict, error) {
	userID, err := s.store.ResolveActor(ctx, config, frame.Actor)
	if err != nil {
		return Verdict{}, fmt.Errorf("resolve inference hook actor: %w", err)
	}
	inputs, err := policyInputs(frame.Messages)
	if err != nil {
		return Verdict{}, fmt.Errorf("decode inference transcript: %w", err)
	}
	if err := s.store.Save(ctx, config, frame, userID); err != nil {
		return Verdict{}, fmt.Errorf("store inference transcript: %w", err)
	}
	verdict := Verdict{Action: "allow", DenyReason: "", ReferenceID: ""}
	for _, input := range inputs {
		result, err := s.scanner.ScanForEnforcement(ctx, config.OrganizationID, config.ProjectID, userID, input.text, input.kind, input.tool)
		if err != nil {
			return Verdict{}, fmt.Errorf("evaluate inference policy: %w", err)
		}
		if result != nil && (result.Action == "block" || result.Action == "warn" || result.Action == "quarantine") {
			// The protocol has no acknowledgement flow; warn policies deny this call.
			verdict.Action = "deny"
			verdict.DenyReason = conv.Default(conv.PtrValOr(result.UserMessage, ""), "This request was blocked by your organization's security policy.")
			return verdict, nil
		}
	}
	return verdict, nil
}

type policyInput struct {
	kind message.Type
	tool string
	text string
}

// Preserve each block as an independent policy input so tool arguments stay
// valid JSON and content-specific policy scope expressions retain their meaning.
func policyInputs(messages []Message) ([]policyInput, error) {
	var inputs []policyInput
	for _, msg := range messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		blocks, err := knownBlocks(msg.Content)
		if err != nil {
			return nil, err
		}
		for _, block := range blocks {
			input := policyInput{kind: "", tool: "", text: ""}
			switch block.Type {
			case "text":
				input.kind, input.text = message.User, block.Text
				if msg.Role == "assistant" {
					input.kind = message.Assistant
				}
			case "attachment":
				input.kind, input.text = message.PromptAttachment, block.Text
			case "tool_use":
				input.kind, input.tool, input.text = message.ToolRequest, block.Name, string(block.Input)
			case "tool_result":
				input.kind, input.tool, input.text = message.ToolResponse, block.ToolName, block.Content
			}
			if input.text == "" && input.tool == "" {
				continue
			}
			inputs = append(inputs, input)
		}
	}
	return inputs, nil
}

type postgresStore struct {
	db     *pgxpool.Pool
	writer *chat.ChatMessageWriter
}

func (s *postgresStore) ResolveActor(ctx context.Context, config Config, actor Actor) (string, error) {
	// Resolve the configured project on every request, including connection tests,
	// so deleted projects and accidentally crossed organization bindings fail closed.
	_, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: config.ProjectID,
		OrganizationID: config.OrganizationID})
	if err != nil {
		return "", fmt.Errorf("validate inference project: %w", err)
	}
	if actor.Type != "user" || actor.EmailAddress == "" {
		return "", nil
	}
	users, err := usersrepo.New(s.db).GetConnectedUsersByEmails(ctx, usersrepo.GetConnectedUsersByEmailsParams{Emails: []string{conv.NormalizeEmail(actor.EmailAddress)},
		OrganizationID: config.OrganizationID})
	if err != nil {
		return "", fmt.Errorf("resolve inference user: %w", err)
	}
	if len(users) == 1 {
		return users[0].ID, nil
	}
	return "", nil
}

func (s *postgresStore) Save(ctx context.Context, config Config, frame Frame, userID string) error {
	if len(frame.Messages) == 0 {
		return nil
	}
	// The external user label is displayed in conversation views. Keep the
	// stable provider actor ID in conversationID, independently of this label.
	// Use the email when present so the conversation header matches its messages,
	// but pass null to the upsert when absent — letting COALESCE preserve a label
	// that was set by an earlier frame rather than overwriting it with the actor ID.
	externalUserIDLabel := conv.NormalizeEmail(frame.Actor.EmailAddress)
	now := time.Now().UTC()
	// Namespacing isolates these opaque, sometimes client-asserted session ids
	// from native hooks and Compliance imports. Null sessions are request-local.
	candidateID := conversationID(config, frame)
	externalChatID := "anthropic-inference:" + candidateID.String()
	chatID, err := chatrepo.New(s.db).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:                candidateID,
		ProjectID:         config.ProjectID,
		OrganizationID:    config.OrganizationID,
		UserID:            conv.ToPGTextEmpty(userID),
		ExternalUserID:    conv.ToPGTextEmpty(externalUserIDLabel),
		ExternalChatID:    conv.ToPGText(externalChatID),
		Title:             conv.ToPGText("Claude inference conversation"),
		CreatedAt:         conv.ToPGTimestamptz(now),
		UpdatedAt:         conv.ToPGTimestamptz(now),
		PreferStoredTitle: true,
	})
	if err != nil {
		return fmt.Errorf("upsert inference conversation: %w", err)
	}
	// When this frame omits the actor email, use the label preserved on the
	// conversation (written by an earlier frame that had one) so new messages
	// stay consistent with the conversation header and existing messages.
	// Fall back to Actor.ID only when no label has been established yet.
	externalUserIDForMessages := externalUserIDLabel
	if externalUserIDForMessages == "" {
		chat, err := chatrepo.New(s.db).GetChat(ctx, chatrepo.GetChatParams{ID: chatID, ProjectID: config.ProjectID})
		if err == nil && chat.ExternalUserID.Valid {
			externalUserIDForMessages = chat.ExternalUserID.String
		} else {
			externalUserIDForMessages = frame.Actor.ID
		}
	}
	if err := chatrepo.New(s.db).UpdateInferenceMessageAttribution(ctx, chatrepo.UpdateInferenceMessageAttributionParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: config.ProjectID, Valid: true}, ActorEmail: conv.ToPGTextEmpty(conv.NormalizeEmail(frame.Actor.EmailAddress)), Source: inferenceSource(frame.Source.Application),
	}); err != nil {
		return fmt.Errorf("refresh inference message attribution: %w", err)
	}

	existing, err := chatrepo.New(s.db).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{ChatID: chatID, ProjectID: config.ProjectID})
	if err != nil {
		return fmt.Errorf("load stored inference message identities: %w", err)
	}
	stored := make(map[string]struct{}, len(existing))
	for _, msg := range existing {
		if msg.ExternalMessageID.Valid {
			stored[msg.ExternalMessageID.String] = struct{}{}
		}
	}
	writes := make([]chat.ExternalMessageWrite, 0, len(frame.Messages))
	for index, msg := range frame.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		id, err := transcriptMessageID(index, msg)
		if err != nil {
			return err
		}
		if _, exists := stored[id]; exists {
			continue
		}
		text, err := messageText(msg)
		if err != nil {
			return fmt.Errorf("render inference message: %w", err)
		}
		writes = append(writes, chat.ExternalMessageWrite{
			Params: chatrepo.CreateExternalChatMessageParams{
				ID:                uuid.Nil,
				ChatID:            chatID,
				Role:              msg.Role,
				ProjectID:         config.ProjectID,
				Content:           text,
				ContentRaw:        msg.Content,
				ContentAssetUrl:   pgtype.Text{String: "", Valid: false},
				StorageError:      pgtype.Text{String: "", Valid: false},
				Model:             conv.ToPGTextEmpty(frame.Model),
				MessageID:         pgtype.Text{String: "", Valid: false},
				ToolCallID:        pgtype.Text{String: "", Valid: false},
				UserID:            conv.ToPGTextEmpty(userID),
				ExternalUserID:    conv.ToPGTextEmpty(externalUserIDForMessages),
				ExternalMessageID: conv.ToPGText(id),
				FinishReason:      pgtype.Text{String: "", Valid: false},
				ToolCalls:         nil,
				PromptTokens:      0,
				CompletionTokens:  0,
				TotalTokens:       0,
				Origin:            conv.ToPGText("anthropic-inference"),
				UserAgent:         pgtype.Text{String: "", Valid: false},
				IpAddress:         pgtype.Text{String: "", Valid: false},
				Source:            conv.ToPGText(inferenceSource(frame.Source.Application)),
				ContentHash:       nil,
				Generation:        0,
				CreatedAt:         conv.ToPGTimestamptz(now.Add(time.Duration(index) * time.Microsecond)),
			},
			BillingUserID:  userID,
			WorkloadSource: metering.WorkloadSourceHook,
			UserEmail:      frame.Actor.EmailAddress,
			Provider:       "anthropic",
			HookHostname:   "",
			AccountType:    "team",
			BillingMode:    "unknown",
		})
	}
	if _, err := s.writer.WriteExternal(ctx, config.ProjectID, writes); err != nil {
		return fmt.Errorf("write inference messages: %w", err)
	}
	return nil
}

// Anthropic has no message ids. A position plus canonical content hash preserves
// identical repeated utterances, deduplicates growing transcript prefixes, and
// retains edited branches. JSON key ordering and whitespace do not change ids.
func transcriptMessageID(index int, msg Message) (string, error) {
	var content any
	decoder := json.NewDecoder(bytes.NewReader(msg.Content))
	decoder.UseNumber()
	if err := decoder.Decode(&content); err != nil {
		return "", fmt.Errorf("decode message identity: %w", err)
	}
	canonical, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("encode message identity: %w", err)
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s", index, strings.TrimSpace(msg.Role), canonical)))
	return "anthropic-inference:" + hex.EncodeToString(hash[:]), nil
}

// Include the actor because Claude Code session ids can be client asserted.
// Neither another actor nor another project can append to this conversation.
func conversationID(config Config, frame Frame) uuid.UUID {
	actorID := conv.Default(frame.Actor.ID, conv.NormalizeEmail(frame.Actor.EmailAddress))
	sessionID := conv.Default(frame.SessionID, frame.RequestID)
	if actorID == "" {
		sessionID = frame.RequestID
	}
	identity, _ := json.Marshal([]string{config.TenantID, frame.Actor.Type, actorID, sessionID})
	return uuid.NewSHA1(config.ProjectID, identity)
}

// inferenceSource maps Anthropic's application names to product surfaces. In
// this protocol claude-code identifies the web product, not the local CLI.
func inferenceSource(application string) string {
	switch source := strings.TrimSpace(application); source {
	case "claude-ai":
		return "claude-chat-web"
	case "claude-code":
		return "claude-code-web"
	case "":
		return "anthropic-inference"
	default:
		return source
	}
}
