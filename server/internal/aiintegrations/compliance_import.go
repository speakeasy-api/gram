package aiintegrations

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

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

	anthropicComplianceSourceWeb     = "Claude Chat Web"
	anthropicComplianceSourceDesktop = "Claude Chat Desktop"

	anthropicCompliancePageLimit          = 1000
	anthropicComplianceActivityPageLimit  = 100
	anthropicComplianceActivityBufferSize = 2 * anthropicComplianceActivityPageLimit
	maxInlineExternalContentSize          = 128 * 1024

	// anthropicComplianceMessagePageBufferSize bounds how many fetched
	// message pages can be queued for writing. Each page holds up to
	// anthropicCompliancePageLimit rows with potentially large raw content,
	// so the buffer is kept small.
	anthropicComplianceMessagePageBufferSize = 2
)

// messagePageBatch is one fetched page of chat messages ready to write.
type messagePageBatch struct {
	chatID uuid.UUID
	rows   []chatrepo.CreateExternalChatMessageParams
	// lastID is the page's pagination token; it advances the per-chat
	// message cursor only after the page's rows are durably written.
	lastID string
}

type ComplianceImportService struct {
	logger         *slog.Logger
	guardianPolicy *guardian.Policy
	db             *pgxpool.Pool
	writer         *chat.ChatMessageWriter
	heartbeat      func(ctx context.Context, scope string, page int)
}

func NewComplianceImportService(logger *slog.Logger, db *pgxpool.Pool, guardianPolicy *guardian.Policy, writer *chat.ChatMessageWriter, heartbeat func(ctx context.Context, scope string, page int)) *ComplianceImportService {
	return &ComplianceImportService{
		logger:         logger.With(attr.SlogComponent("aiintegrations.anthropic_compliance")),
		guardianPolicy: guardianPolicy,
		db:             db,
		writer:         writer,
		heartbeat:      heartbeat,
	}
}

// SyncAnthropicCompliance imports new compliance activity and chat messages.
// It returns the activities pagination cursor reached by this run; callers
// must persist it on success so the next run resumes after the last imported
// activity instead of re-discovering by time window.
//
// The sync is a three-stage pipeline: a producer goroutine streams chat
// activities from the feed page by page, a fetch goroutine resolves each chat
// and pages its messages into row batches, and a writer goroutine persists
// the batches. This bounds memory to the channel buffers and overlaps API
// paging with the batch inserts.
func (s *ComplianceImportService) SyncAnthropicCompliance(ctx context.Context, cfg Config) (string, error) {
	if cfg.Provider != ProviderAnthropicCompliance {
		return "", oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for compliance import: %s", cfg.Provider)
	}
	if cfg.ExternalOrganizationID == nil {
		return "", oops.E(oops.CodeInvalid, nil, "external_organization_id is required for anthropic_compliance")
	}

	client := anthropicapi.New(s.guardianPolicy, anthropicapi.WithAPIKey(cfg.APIKey))

	g, gctx := errgroup.WithContext(ctx)
	chatActivities := make(chan anthropicapi.Activity, anthropicComplianceActivityBufferSize)
	messagePages := make(chan messagePageBatch, anthropicComplianceMessagePageBufferSize)
	fetchErr := make(chan error, 1)

	// Written by the producer goroutine; only read after g.Wait.
	var nextCursor string

	g.Go(func() (err error) {
		defer close(chatActivities)
		defer func() {
			fetchErr <- err
			close(fetchErr)
		}()
		nextCursor, err = s.streamChatActivities(gctx, client, cfg, chatActivities)
		return err
	})

	g.Go(func() (err error) {
		defer close(messagePages)

		users := newConnectedUserResolver(s.db, cfg.OrganizationID)
		// It doesn't matter if the chat is imported more than once per run,
		// because chat inserts are idempotent.
		for activity := range chatActivities {
			s.heartbeat(gctx, "chat_import", 0)
			chatID, messagesCursor, err := s.upsertActivityChat(gctx, cfg, activity, users)
			if err != nil {
				return err
			}

			// Chat metadata (title, owner, timestamps) is set once when the
			// chat is first seen via its created activity; it doesn't change
			// on later claude_chat_updated activities.
			enrichChat := activity.Type == anthropicComplianceActivityCreated
			if err := s.fetchChatMessages(gctx, client, cfg, chatID, activity, messagesCursor, enrichChat, users, messagePages); err != nil {
				return err
			}
		}
		if err := <-fetchErr; err != nil {
			return err
		}
		return nil
	})

	// Writer stage. It must remain a single goroutine: the per-chat message
	// cursor may only advance after that page's rows are durably written,
	// and pages of one chat must be written in feed order.
	g.Go(func() error {
		pageNum := 0
		for batch := range messagePages {
			pageNum++
			s.heartbeat(gctx, "message_write", pageNum)

			if _, err := s.writer.WriteExternal(gctx, cfg.ProjectID, batch.rows); err != nil {
				return oops.E(oops.CodeUnexpected, err, "write anthropic compliance chat messages")
			}

			if batch.lastID != "" {
				if err := chatrepo.New(s.db).UpdateAIIntegrationConfigChatCursor(gctx, chatrepo.UpdateAIIntegrationConfigChatCursorParams{
					LastCursorID: conv.ToPGText(batch.lastID),
					ChatID:       batch.chatID,
				}); err != nil {
					return oops.E(oops.CodeUnexpected, err, "record anthropic compliance chat cursor")
				}
			}
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return "", err //nolint:wrapcheck // Preserve the original goroutine error for callers.
	}
	return nextCursor, nil
}

// streamChatActivities pages through the activities feed starting after the
// persisted cursor and sends chat activities to out as they are discovered.
// It returns the cursor reached by this run: the verbatim last_id pagination
// token from the final page, not an id derived from imported rows.
func (s *ComplianceImportService) streamChatActivities(ctx context.Context, client *anthropicapi.Client, cfg Config, out chan<- anthropicapi.Activity) (string, error) {
	// First sync has no cursor yet; bound the initial backfill with a time
	// window around the watermark. Every later sync resumes from the cursor.
	createdAtGTE := time.Time{}
	if cfg.LastCursor == "" {
		createdAtGTE = cfg.PollWatermarkAt.Add(-time.Hour * 24)
	}

	afterID := cfg.LastCursor
	nextCursor := cfg.LastCursor
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "activity_discovery", pageNum)
		page, err := client.ListActivities(ctx, anthropicapi.ListActivitiesParams{
			ActivityTypes: []string{
				anthropicComplianceActivityCreated,
				anthropicComplianceActivityUpdated,
			},
			OrganizationIDs: []string{*cfg.ExternalOrganizationID},
			CreatedAtGTE:    createdAtGTE,
			AfterID:         afterID,
			Limit:           anthropicComplianceActivityPageLimit,
		})
		if err != nil {
			return nextCursor, oops.E(oops.CodeUnexpected, err, "list anthropic compliance activities")
		}

		for _, activity := range page.Data {
			if activity.Actor.Type != "user_actor" || activity.ClaudeChatID == "" {
				continue
			}
			select {
			case <-ctx.Done():
				return nextCursor, ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
			case out <- activity:
			}
		}

		if page.LastID != "" {
			nextCursor = page.LastID
		}

		if !page.HasMore || page.LastID == "" {
			return nextCursor, nil
		}
		afterID = page.LastID
	}
}

// upsertActivityChat resolves the chat row for an activity and returns its
// persisted message pagination cursor alongside the chat id.
func (s *ComplianceImportService) upsertActivityChat(ctx context.Context, cfg Config, activity anthropicapi.Activity, users *connectedUserResolver) (uuid.UUID, string, error) {
	createdAt, err := activity.CreatedAtTime()
	if err != nil {
		return uuid.Nil, "", oops.E(oops.CodeUnexpected, err, "parse anthropic compliance activity timestamp")
	}

	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	userID, err := users.resolve(ctx, activity.Actor.EmailAddress)
	if err != nil {
		return uuid.Nil, "", err
	}
	chatID, err := chatrepo.New(s.db).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             uuid.New(),
		ProjectID:      cfg.ProjectID,
		OrganizationID: cfg.OrganizationID,
		UserID:         conv.ToPGText(userID),
		ExternalUserID: conv.ToPGText(activity.Actor.UserID),
		ExternalChatID: conv.ToPGText(activity.ClaudeChatID),
		// NULL so the upsert's COALESCE never clobbers the real title set by
		// the one-time enrichment from the chat's first message page.
		Title:     pgtype.Text{String: "", Valid: false},
		CreatedAt: conv.ToPGTimestamptz(createdAt),
		UpdatedAt: conv.ToPGTimestamptz(createdAt),
	})
	if err != nil {
		return uuid.Nil, "", oops.E(oops.CodeUnexpected, err, "upsert anthropic compliance chat")
	}
	messagesCursor, err := chatrepo.New(s.db).LinkAIIntegrationConfigChat(ctx, chatrepo.LinkAIIntegrationConfigChatParams{
		AiIntegrationConfigID: cfg.ID,
		ChatID:                chatID,
	})
	if err != nil {
		return uuid.Nil, "", oops.E(oops.CodeUnexpected, err, "link anthropic compliance chat")
	}
	return chatID, messagesCursor.String, nil
}

// fetchChatMessages pages through a chat's messages starting after the
// chat's persisted cursor and sends each page's rows to out for the writer
// stage to persist. The writer advances the per-chat cursor after every
// successfully written page so a failed run resumes at the last completed
// page instead of re-importing the whole chat.
//
// When enrichChat is set, the first page's chat-level metadata (title,
// owner, timestamps) is upserted onto the chat row; the metadata is
// identical on every page, so once is enough.
func (s *ComplianceImportService) fetchChatMessages(ctx context.Context, client *anthropicapi.Client, cfg Config, chatID uuid.UUID, activity anthropicapi.Activity, afterID string, enrichChat bool, users *connectedUserResolver, out chan<- messagePageBatch) error {
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "message_import", pageNum)
		page, err := client.GetChatMessages(ctx, anthropicapi.GetChatMessagesParams{
			ClaudeChatID: activity.ClaudeChatID,
			AfterID:      afterID,
			Limit:        anthropicCompliancePageLimit,
		})

		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "get anthropic compliance chat messages")
		}

		if enrichChat && pageNum == 1 {
			if err := s.upsertMessagePageChat(ctx, cfg, chatID, page, users); err != nil {
				return err
			}
		}

		rows, err := s.buildExternalMessageRows(ctx, cfg, chatID, page, activity, users)
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
		case out <- messagePageBatch{chatID: chatID, rows: rows, lastID: page.LastID}:
		}

		if !page.HasMore || page.LastID == "" {
			break
		}
		afterID = page.LastID
	}
	return nil
}

func (s *ComplianceImportService) upsertMessagePageChat(ctx context.Context, cfg Config, chatID uuid.UUID, page *anthropicapi.ChatMessagesPage, users *connectedUserResolver) error {
	createdAt := parseTimeOrDefault(page.CreatedAt, time.Now().UTC())
	updatedAt := parseTimeOrDefault(page.UpdatedAt, createdAt)
	userID, err := users.resolve(ctx, page.User.EmailAddress)
	if err != nil {
		return err
	}
	resolvedChatID, err := chatrepo.New(s.db).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             chatID,
		ProjectID:      cfg.ProjectID,
		OrganizationID: cfg.OrganizationID,
		UserID:         conv.ToPGText(userID),
		ExternalUserID: conv.ToPGText(page.User.ID),
		ExternalChatID: conv.ToPGText(page.ID),
		Title:          conv.ToPGText(page.Name),
		CreatedAt:      conv.ToPGTimestamptz(createdAt),
		UpdatedAt:      conv.ToPGTimestamptz(updatedAt),
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

func (s *ComplianceImportService) buildExternalMessageRows(ctx context.Context, cfg Config, chatID uuid.UUID, page *anthropicapi.ChatMessagesPage, activity anthropicapi.Activity, users *connectedUserResolver) ([]chatrepo.CreateExternalChatMessageParams, error) {
	rows := make([]chatrepo.CreateExternalChatMessageParams, 0, len(page.Messages))
	userID, err := users.resolve(ctx, page.User.EmailAddress)
	if err != nil {
		return nil, err
	}
	source := complianceSourceFromUserAgent(activity.Actor.UserAgent)
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
			ContentAssetUrl:   pgtype.Text{String: "", Valid: false},
			StorageError:      pgtype.Text{String: "", Valid: false},
			Model:             conv.ToPGText(model),
			MessageID:         pgtype.Text{String: "", Valid: false},
			ToolCallID:        pgtype.Text{String: "", Valid: false},
			UserID:            conv.ToPGText(userID),
			ExternalUserID:    conv.ToPGText(page.User.ID),
			ExternalMessageID: conv.ToPGText(msg.ID),
			FinishReason:      pgtype.Text{String: "", Valid: false},
			ToolCalls:         nil,
			PromptTokens:      0,
			CompletionTokens:  0,
			TotalTokens:       0,
			Origin:            conv.ToPGText(page.Href),
			UserAgent:         conv.ToPGTextEmpty(activity.Actor.UserAgent),
			IpAddress:         conv.ToPGTextEmpty(activity.Actor.IPAddress),
			Source:            conv.ToPGText(source),
			ContentHash:       nil,
			Generation:        0,
			CreatedAt:         conv.ToPGTimestamptz(createdAt),
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

// connectedUserResolver lazily maps actor emails to connected user ids
// within the organization, caching lookups (including misses) for the
// duration of one sync run. It is only used from the fetch goroutine and
// is not safe for concurrent use.
type connectedUserResolver struct {
	users *usersrepo.Queries
	orgID string
	cache map[string]string
}

func newConnectedUserResolver(db *pgxpool.Pool, orgID string) *connectedUserResolver {
	return &connectedUserResolver{
		users: usersrepo.New(db),
		orgID: orgID,
		cache: map[string]string{},
	}
}

// resolve returns the connected user id for an email, or "" when the email
// is empty or not connected to the organization.
func (r *connectedUserResolver) resolve(ctx context.Context, email string) (string, error) {
	email = conv.NormalizeEmail(email)
	if email == "" {
		return "", nil
	}
	if userID, ok := r.cache[email]; ok {
		return userID, nil
	}

	users, err := r.users.GetConnectedUsersByEmails(ctx, usersrepo.GetConnectedUsersByEmailsParams{
		Emails:         []string{email},
		OrganizationID: r.orgID,
	})
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "hydrate anthropic compliance user")
	}

	r.cache[email] = ""
	for _, user := range users {
		r.cache[conv.NormalizeEmail(user.Email)] = user.ID
	}
	return r.cache[email], nil
}

// complianceSourceFromUserAgent classifies the activity actor's user agent.
// The Claude desktop app is an Electron shell whose user agent carries
// "Claude/<version>" and "Electron/<version>" product tokens; plain browser
// user agents (Claude web) have neither.
func complianceSourceFromUserAgent(userAgent string) string {
	if strings.Contains(userAgent, "Claude/") || strings.Contains(userAgent, "Electron/") {
		return anthropicComplianceSourceDesktop
	}
	return anthropicComplianceSourceWeb
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
