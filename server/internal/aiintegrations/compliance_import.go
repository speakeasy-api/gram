package aiintegrations

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	anthropicComplianceActivityCreated = "claude_chat_created"
	anthropicComplianceActivityUpdated = "claude_chat_updated"
	anthropicComplianceSource          = "anthropic_compliance"
	anthropicCompliancePageLimit       = 1000
	maxInlineExternalContentSize       = 128 * 1024
)

type ComplianceImportService struct {
	logger         *slog.Logger
	guardianPolicy *guardian.Policy
	chatRepo       *chatrepo.Queries
	users          *usersrepo.Queries
	writer         *chat.ChatMessageWriter
	heartbeat      func(ctx context.Context, scope string, page int)
}

func NewComplianceImportService(logger *slog.Logger, db *pgxpool.Pool, guardianPolicy *guardian.Policy, writer *chat.ChatMessageWriter, heartbeat func(ctx context.Context, scope string, page int)) *ComplianceImportService {
	return &ComplianceImportService{
		logger:         logger.With(attr.SlogComponent("aiintegrations.anthropic_compliance")),
		guardianPolicy: guardianPolicy,
		chatRepo:       chatrepo.New(db),
		users:          usersrepo.New(db),
		writer:         writer,
		heartbeat:      heartbeat,
	}
}

// SyncAnthropicCompliance imports new compliance activity and chat messages.
// It returns the activities pagination cursor reached by this run; callers
// must persist it on success so the next run resumes after the last imported
// activity instead of re-discovering by time window.
func (s *ComplianceImportService) SyncAnthropicCompliance(ctx context.Context, cfg Config) (string, error) {
	if cfg.Provider != ProviderAnthropicCompliance {
		return "", oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for compliance import: %s", cfg.Provider)
	}
	if cfg.ExternalOrganizationID == "" {
		return "", oops.E(oops.CodeInvalid, nil, "external_organization_id is required for anthropic_compliance")
	}

	client := anthropicapi.New(s.guardianPolicy, anthropicapi.WithAPIKey(cfg.APIKey))
	activities, nextCursor, err := s.discoverChatActivities(ctx, client, cfg)
	if err != nil {
		return "", err
	}
	if len(activities) == 0 {
		return nextCursor, nil
	}

	userIDsByEmail, err := s.hydrateConnectedUsers(ctx, cfg.OrganizationID, activityEmails(activities))
	if err != nil {
		return "", err
	}

	for _, activity := range activities {
		s.heartbeat(ctx, "chat_import", 0)
		chatID, messagesCursor, err := s.upsertActivityChat(ctx, cfg, activity, userIDsByEmail)
		if err != nil {
			return "", err
		}
		if err := s.importChatMessages(ctx, client, cfg, chatID, activity.ClaudeChatID, messagesCursor, userIDsByEmail); err != nil {
			return "", err
		}
	}
	return nextCursor, nil
}

// discoverChatActivities pages through the activities feed starting after the
// persisted cursor and returns the deduplicated chat activities plus the
// cursor reached by this run. The cursor is the verbatim last_id pagination
// token from the final page, not an id derived from imported rows.
func (s *ComplianceImportService) discoverChatActivities(ctx context.Context, client *anthropicapi.Client, cfg Config) ([]anthropicapi.Activity, string, error) {
	// First sync has no cursor yet; bound the initial backfill with a time
	// window around the watermark. Every later sync resumes from the cursor.
	createdAtGTE := time.Time{}
	if cfg.LastCursor == "" {
		createdAtGTE = cfg.PollWatermarkAt.Add(-time.Hour * 24)
	}

	seen := map[string]anthropicapi.Activity{}
	afterID := cfg.LastCursor
	nextCursor := cfg.LastCursor
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "activity_discovery", pageNum)
		page, err := client.ListActivities(ctx, anthropicapi.ListActivitiesParams{
			ActivityTypes: []string{
				anthropicComplianceActivityCreated,
				anthropicComplianceActivityUpdated,
			},
			OrganizationIDs: []string{cfg.ExternalOrganizationID},
			CreatedAtGTE:    createdAtGTE,
			AfterID:         afterID,
			Limit:           5000,
		})
		if err != nil {
			return nil, "", oops.E(oops.CodeUnexpected, err, "list anthropic compliance activities")
		}
		for _, activity := range page.Data {
			if activity.Actor.Type != "user_actor" || activity.ClaudeChatID == "" {
				continue
			}
			seen[activity.ClaudeChatID] = activity
		}
		if page.LastID != "" {
			nextCursor = page.LastID
		}
		if !page.HasMore || page.LastID == "" {
			break
		}
		afterID = page.LastID
	}

	activities := make([]anthropicapi.Activity, 0, len(seen))
	for _, activity := range seen {
		activities = append(activities, activity)
	}
	return activities, nextCursor, nil
}

// upsertActivityChat resolves the chat row for an activity and returns its
// persisted message pagination cursor alongside the chat id.
func (s *ComplianceImportService) upsertActivityChat(ctx context.Context, cfg Config, activity anthropicapi.Activity, userIDsByEmail map[string]string) (uuid.UUID, string, error) {
	createdAt, err := activity.CreatedAtTime()
	if err != nil {
		return uuid.Nil, "", oops.E(oops.CodeUnexpected, err, "parse anthropic compliance activity timestamp")
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	userID := userIDsByEmail[normalizeEmail(activity.Actor.EmailAddress)]
	chatID, err := s.chatRepo.UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             uuid.New(),
		ProjectID:      cfg.ProjectID,
		OrganizationID: cfg.OrganizationID,
		UserID:         conv.ToPGText(userID),
		ExternalUserID: conv.ToPGText(activity.Actor.UserID),
		ExternalChatID: conv.ToPGText(activity.ClaudeChatID),
		Title:          conv.ToPGText("Claude Chat"),
		CreatedAt:      timestamptz(createdAt),
		UpdatedAt:      timestamptz(createdAt),
	})
	if err != nil {
		return uuid.Nil, "", oops.E(oops.CodeUnexpected, err, "upsert anthropic compliance chat")
	}
	messagesCursor, err := s.chatRepo.LinkAIIntegrationConfigChat(ctx, chatrepo.LinkAIIntegrationConfigChatParams{
		AiIntegrationConfigID: cfg.ID,
		ChatID:                chatID,
	})
	if err != nil {
		return uuid.Nil, "", oops.E(oops.CodeUnexpected, err, "link anthropic compliance chat")
	}
	return chatID, messagesCursor.String, nil
}

// importChatMessages pages through a chat's messages starting after the
// chat's persisted cursor. The cursor is advanced after every successfully
// written page so a failed run resumes at the last completed page instead of
// re-importing the whole chat.
func (s *ComplianceImportService) importChatMessages(ctx context.Context, client *anthropicapi.Client, cfg Config, chatID uuid.UUID, claudeChatID string, afterID string, userIDsByEmail map[string]string) error {
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "message_import", pageNum)
		page, err := client.GetChatMessages(ctx, anthropicapi.GetChatMessagesParams{
			ClaudeChatID: claudeChatID,
			AfterID:      afterID,
			Limit:        anthropicCompliancePageLimit,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "get anthropic compliance chat messages")
		}
		if err := s.upsertMessagePageChat(ctx, cfg, chatID, page, userIDsByEmail); err != nil {
			return err
		}
		rows, err := s.buildExternalMessageRows(cfg, chatID, page, userIDsByEmail)
		if err != nil {
			return err
		}
		if _, err := s.writer.WriteExternal(ctx, cfg.ProjectID, rows); err != nil {
			return oops.E(oops.CodeUnexpected, err, "write anthropic compliance chat messages")
		}
		if page.LastID != "" {
			if err := s.chatRepo.UpdateAIIntegrationConfigChatCursor(ctx, chatrepo.UpdateAIIntegrationConfigChatCursorParams{
				LastCursorID: conv.ToPGText(page.LastID),
				ChatID:       chatID,
			}); err != nil {
				return oops.E(oops.CodeUnexpected, err, "record anthropic compliance chat cursor")
			}
		}
		if !page.HasMore || page.LastID == "" {
			break
		}
		afterID = page.LastID
	}
	return nil
}

func (s *ComplianceImportService) upsertMessagePageChat(ctx context.Context, cfg Config, chatID uuid.UUID, page *anthropicapi.ChatMessagesPage, userIDsByEmail map[string]string) error {
	createdAt := parseTimeOrDefault(page.CreatedAt, time.Now().UTC())
	updatedAt := parseTimeOrDefault(page.UpdatedAt, createdAt)
	userID := userIDsByEmail[normalizeEmail(page.User.EmailAddress)]
	resolvedChatID, err := s.chatRepo.UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             chatID,
		ProjectID:      cfg.ProjectID,
		OrganizationID: cfg.OrganizationID,
		UserID:         conv.ToPGText(userID),
		ExternalUserID: conv.ToPGText(page.User.ID),
		ExternalChatID: conv.ToPGText(page.ID),
		Title:          conv.ToPGText(page.Name),
		CreatedAt:      timestamptz(createdAt),
		UpdatedAt:      timestamptz(updatedAt),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "upsert anthropic compliance chat metadata")
	}
	if resolvedChatID != chatID {
		s.logger.WarnContext(ctx, "anthropic compliance chat resolved to different id",
			attr.SlogChatID(resolvedChatID.String()),
			attr.SlogAIIntegrationConfigID(cfg.ID.String()),
		)
	}
	return nil
}

func (s *ComplianceImportService) buildExternalMessageRows(cfg Config, chatID uuid.UUID, page *anthropicapi.ChatMessagesPage, userIDsByEmail map[string]string) ([]chatrepo.CreateExternalChatMessageParams, error) {
	rows := make([]chatrepo.CreateExternalChatMessageParams, 0, len(page.Messages))
	userID := userIDsByEmail[normalizeEmail(page.User.EmailAddress)]
	model := ""
	if page.Model != nil {
		model = *page.Model
	}
	for _, msg := range page.Messages {
		if msg.ID == "" || (msg.Role != "user" && msg.Role != "assistant") {
			continue
		}
		createdAt, err := msg.CreatedAtTime()
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "parse anthropic compliance message timestamp")
		}
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}

		content := renderComplianceContent(msg.Content)
		var contentRaw []byte
		if len(msg.Content) > 0 && len(msg.Content) <= maxInlineExternalContentSize {
			contentRaw = msg.Content
		}
		rows = append(rows, chatrepo.CreateExternalChatMessageParams{
			ChatID:            chatID,
			Role:              msg.Role,
			ProjectID:         cfg.ProjectID,
			Content:           content,
			ContentRaw:        contentRaw,
			ContentAssetUrl:   pgtype.Text{},
			StorageError:      pgtype.Text{},
			Model:             conv.ToPGText(model),
			MessageID:         pgtype.Text{},
			ToolCallID:        pgtype.Text{},
			UserID:            conv.ToPGText(userID),
			ExternalUserID:    conv.ToPGText(page.User.ID),
			ExternalMessageID: conv.ToPGText(msg.ID),
			FinishReason:      pgtype.Text{},
			ToolCalls:         nil,
			PromptTokens:      0,
			CompletionTokens:  0,
			TotalTokens:       0,
			Origin:            conv.ToPGText(page.Href),
			UserAgent:         pgtype.Text{},
			IpAddress:         pgtype.Text{},
			Source:            conv.ToPGText(anthropicComplianceSource),
			ContentHash:       nil,
			Generation:        0,
			CreatedAt:         timestamptz(createdAt),
		})
	}
	return rows, nil
}

type complianceContentBlock struct {
	Type            string                   `json:"type"`
	Text            string                   `json:"text"`
	ID              string                   `json:"id"`
	Name            string                   `json:"name"`
	Input           json.RawMessage          `json:"input"`
	IntegrationName string                   `json:"integration_name"`
	MCPServerURL    string                   `json:"mcp_server_url"`
	ToolUseID       string                   `json:"tool_use_id"`
	IsError         bool                     `json:"is_error"`
	Truncated       bool                     `json:"truncated"`
	Content         []complianceContentBlock `json:"content"`
}

func renderComplianceContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var blocks []complianceContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return string(raw)
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "tool_use":
			detail := strings.TrimSpace(block.Name)
			if len(block.Input) > 0 {
				detail = strings.TrimSpace(detail + " " + string(block.Input))
			}
			parts = append(parts, strings.TrimSpace("[tool_use "+detail+"]"))
		case "tool_result":
			texts := make([]string, 0, len(block.Content))
			for _, item := range block.Content {
				if item.Type == "text" && item.Text != "" {
					texts = append(texts, item.Text)
				}
			}
			if len(texts) > 0 {
				parts = append(parts, strings.Join(texts, "\n"))
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func (s *ComplianceImportService) hydrateConnectedUsers(ctx context.Context, orgID string, emails []string) (map[string]string, error) {
	out := map[string]string{}
	if len(emails) == 0 {
		return out, nil
	}
	users, err := s.users.GetConnectedUsersByEmails(ctx, usersrepo.GetConnectedUsersByEmailsParams{
		Emails:         emails,
		OrganizationID: orgID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "hydrate anthropic compliance users")
	}
	for _, user := range users {
		out[normalizeEmail(user.Email)] = user.ID
	}
	return out, nil
}

func activityEmails(activities []anthropicapi.Activity) []string {
	seen := map[string]struct{}{}
	for _, activity := range activities {
		email := normalizeEmail(activity.Actor.EmailAddress)
		if email != "" {
			seen[email] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for email := range seen {
		out = append(out, email)
	}
	return out
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func parseTimeOrDefault(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback.UTC()
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fallback.UTC()
	}
	return t.UTC()
}

func IsAnthropicComplianceUnauthorized(err error) bool {
	var httpErr *anthropicapi.HTTPError
	return errors.As(err, &httpErr) && (httpErr.StatusCode == 401 || httpErr.StatusCode == 403)
}
