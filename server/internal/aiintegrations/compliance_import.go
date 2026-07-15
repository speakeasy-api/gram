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

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
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

// discoveredActivity is one importable chat activity from the feed.
type discoveredActivity struct {
	activity anthropicapi.Activity
	// activitiesCursor is set on the final importable activity of an
	// activities page during forward walks: once that activity's messages
	// are durably written, the whole page is fully imported and its first_id
	// token may be persisted as the global activities cursor.
	activitiesCursor string
	// cursorOnly marks a sentinel for a page with no importable activities.
	// It carries just the page's pagination token; the import stage forwards
	// it to the writer without fetching anything so the cursor still
	// advances past fully-filtered pages.
	cursorOnly bool
}

// messagePageBatch is one fetched page of chat messages ready to write.
type messagePageBatch struct {
	chatID uuid.UUID
	rows   []chatrepo.CreateExternalChatMessageParams
	// lastID is the page's pagination token; it advances the per-chat
	// message cursor only after the page's rows are durably written.
	lastID string
	// activitiesCursor, when set, marks this batch as the last one produced
	// by an activities page; the writer persists it as the global activities
	// cursor once the batch is durably written, so a failed run resumes from
	// the last completed activities page instead of the stored cursor from
	// the previous successful sync.
	activitiesCursor string
	// cursorOnly marks a sentinel batch that carries just an activities
	// cursor for a fully-filtered page; there is nothing to write.
	cursorOnly bool
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
// It returns the activities feed position reached by this run: the newest
// first_id token seen. Callers must persist it on success so the next run
// walks forward (before_id) from that position — the feed is sorted newest
// first, so this is the only direction that ever discovers new activity.
// During forward walks the cursor is also persisted incrementally — after
// each activities page whose chats are fully written — so retries of a
// failed or timed-out run resume from the last completed page rather than
// repaying the whole discovery cost.
//
// The sync is a three-stage pipeline: a producer goroutine streams chat
// activities from the feed page by page, a fetch goroutine resolves each chat
// and pages its messages into row batches, and a writer goroutine persists
// the batches. This bounds memory to the channel buffers and overlaps API
// paging with the batch inserts.
//
// On failure the returned error is a SyncError that accumulates every
// stage's failure alongside the progress the run made, so one report tells
// the whole story instead of only the first error to win the race.
func (s *ComplianceImportService) SyncAnthropicCompliance(ctx context.Context, cfg Config) (string, error) {
	if cfg.Provider != ProviderAnthropicCompliance {
		return "", oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for compliance import: %s", cfg.Provider)
	}
	if cfg.ExternalOrganizationID == nil {
		return "", oops.E(oops.CodeInvalid, nil, "external_organization_id is required for anthropic_compliance")
	}

	client := anthropicapi.New(s.guardianPolicy, anthropicapi.WithAPIKey(cfg.APIKey))

	g, gctx := errgroup.WithContext(ctx)
	chatActivities := make(chan discoveredActivity, anthropicComplianceActivityBufferSize)
	messagePages := make(chan messagePageBatch, anthropicComplianceMessagePageBufferSize)

	// Each stage writes only its own progress fields and error variable;
	// everything is read after g.Wait, which establishes the happens-before.
	progress := &ComplianceSyncProgress{
		FirstSync:           cfg.LastCursor == "",
		ActivityPages:       0,
		ChatActivities:      0,
		ChatsImported:       0,
		MessagePagesFetched: 0,
		MessagePagesWritten: 0,
		CursorReached:       "",
		CursorPersisted:     "",
	}
	var nextCursor string
	var discoverErr, importErr, writeErr error

	g.Go(func() error {
		defer close(chatActivities)
		nextCursor, discoverErr = s.streamChatActivities(gctx, client, cfg, chatActivities, progress)
		return discoverErr
	})

	g.Go(func() error {
		defer close(messagePages)
		importErr = s.importChatActivities(gctx, client, cfg, chatActivities, messagePages, progress)
		return importErr
	})

	g.Go(func() error {
		writeErr = s.writeMessagePages(gctx, cfg, messagePages, progress)
		return writeErr
	})

	if err := g.Wait(); err != nil {
		progress.CursorReached = nextCursor
		return "", newSyncError("sync anthropic compliance", *progress,
			SyncStageError{Stage: "discover_activities", Err: discoverErr},
			SyncStageError{Stage: "import_chats", Err: importErr},
			SyncStageError{Stage: "write_messages", Err: writeErr},
		)
	}
	return nextCursor, nil
}

// importChatActivities consumes discovered chat activities, upserts the chat
// rows, and pages each chat's messages into row batches for the writer
// stage. It doesn't matter if a chat is imported more than once per run,
// because chat inserts are idempotent.
func (s *ComplianceImportService) importChatActivities(ctx context.Context, client *anthropicapi.Client, cfg Config, in <-chan discoveredActivity, out chan<- messagePageBatch, progress *ComplianceSyncProgress) error {
	users := newConnectedUserResolver(s.db, cfg.OrganizationID)
	for discovered := range in {
		if discovered.cursorOnly {
			// A fully-filtered page has no chats to import; forward its
			// cursor straight to the writer so it still becomes durable
			// after all previously discovered work is written.
			select {
			case <-ctx.Done():
				return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
			case out <- messagePageBatch{chatID: uuid.Nil, rows: nil, lastID: "", activitiesCursor: discovered.activitiesCursor, cursorOnly: true}:
			}
			continue
		}

		activity := discovered.activity
		s.heartbeat(ctx, "chat_import", progress.ChatsImported)
		chatID, messagesCursor, err := s.upsertActivityChat(ctx, cfg, activity, users)
		if err != nil {
			return err
		}
		progress.ChatsImported++

		// Chat metadata (title, owner, timestamps) is set once when the
		// chat is first seen via its created activity; it doesn't change
		// on later claude_chat_updated activities.
		enrichChat := activity.Type == anthropicComplianceActivityCreated
		if err := s.fetchChatMessages(ctx, client, cfg, chatID, activity, messagesCursor, enrichChat, discovered.activitiesCursor, users, out, progress); err != nil {
			return err
		}
	}
	return nil
}

// writeMessagePages persists fetched message pages. It must remain a single
// goroutine: the per-chat message cursor may only advance after that page's
// rows are durably written, and pages of one chat must be written in feed
// order. When a batch closes out an activities page, the global activities
// cursor is advanced so retries resume from the last completed page.
func (s *ComplianceImportService) writeMessagePages(ctx context.Context, cfg Config, in <-chan messagePageBatch, progress *ComplianceSyncProgress) error {
	for batch := range in {
		if !batch.cursorOnly {
			s.heartbeat(ctx, "message_write", progress.MessagePagesWritten+1)

			if _, err := s.writer.WriteExternal(ctx, cfg.ProjectID, batch.rows); err != nil {
				return oops.E(oops.CodeUnexpected, err, "write anthropic compliance chat messages")
			}

			if batch.lastID != "" {
				if err := chatrepo.New(s.db).UpdateAIIntegrationConfigChatCursor(ctx, chatrepo.UpdateAIIntegrationConfigChatCursorParams{
					LastCursorID: conv.ToPGText(batch.lastID),
					ChatID:       batch.chatID,
				}); err != nil {
					return oops.E(oops.CodeUnexpected, err, "record anthropic compliance chat cursor")
				}
			}
			progress.MessagePagesWritten++
		}

		// Batches arrive in feed order, so once the last batch of an
		// activities page is written, everything discovered up to that
		// page's pagination token is durable and the token is a safe
		// resume point.
		if batch.activitiesCursor != "" {
			if err := repo.New(s.db).AdvanceUsagePollCursor(ctx, repo.AdvanceUsagePollCursorParams{
				LastCursorID:          conv.ToPGText(batch.activitiesCursor),
				AiIntegrationConfigID: cfg.ID,
			}); err != nil {
				return oops.E(oops.CodeUnexpected, err, "advance anthropic compliance activities cursor")
			}
			progress.CursorPersisted = batch.activitiesCursor
		}
	}
	return nil
}

// streamChatActivities discovers importable chat activities and sends them
// to out. The activities feed is always sorted newest first, so the walk
// direction depends on cursor state:
//
//   - First sync (no cursor): a bounded backfill pages OLDER via after_id
//     down to the watermark window. It returns the newest edge seen so all
//     later syncs walk forward from there.
//   - Every later sync: pages NEWER via before_id from the stored cursor
//     and returns the newest first_id reached.
//
// During forward walks the final importable activity of each page carries
// the page's first_id so the writer stage persists the global cursor once
// the page is durably written. Backfill pages carry no markers: an
// interrupted first sync restarts its bounded window instead of risking a
// forward resume that would skip the window's unimported older tail.
func (s *ComplianceImportService) streamChatActivities(ctx context.Context, client *anthropicapi.Client, cfg Config, out chan<- discoveredActivity, progress *ComplianceSyncProgress) (string, error) {
	if cfg.LastCursor == "" {
		return s.backfillChatActivities(ctx, client, cfg, out, progress)
	}

	beforeID := cfg.LastCursor
	nextCursor := cfg.LastCursor
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "activity_discovery", pageNum)
		page, err := client.ListActivities(ctx, anthropicapi.ListActivitiesParams{
			ActivityTypes: []string{
				anthropicComplianceActivityCreated,
				anthropicComplianceActivityUpdated,
			},
			OrganizationIDs: []string{*cfg.ExternalOrganizationID},
			CreatedAtGTE:    time.Time{},
			AfterID:         "",
			BeforeID:        beforeID,
			Limit:           anthropicComplianceActivityPageLimit,
		})
		if err != nil {
			return nextCursor, oops.E(oops.CodeUnexpected, err, "list anthropic compliance activities")
		}
		progress.ActivityPages++

		if err := s.emitPageActivities(ctx, page, page.FirstID, out, progress); err != nil {
			return nextCursor, err
		}

		if page.FirstID != "" {
			nextCursor = page.FirstID
		}

		if !page.HasMore || page.FirstID == "" {
			return nextCursor, nil
		}
		beforeID = page.FirstID
	}
}

// backfillChatActivities bootstraps a config that has no cursor yet by
// paging OLDER via after_id, bounded to a time window around the watermark.
// It returns the newest edge of the feed seen — the first page's first_id —
// so the next sync walks forward from there with before_id.
func (s *ComplianceImportService) backfillChatActivities(ctx context.Context, client *anthropicapi.Client, cfg Config, out chan<- discoveredActivity, progress *ComplianceSyncProgress) (string, error) {
	createdAtGTE := cfg.PollWatermarkAt.Add(-time.Hour * 24)

	afterID := ""
	nextCursor := ""
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
			BeforeID:        "",
			Limit:           anthropicComplianceActivityPageLimit,
		})
		if err != nil {
			return nextCursor, oops.E(oops.CodeUnexpected, err, "list anthropic compliance activities")
		}
		progress.ActivityPages++

		// The feed is newest first, so the first page's first_id is the
		// newest activity this run can see; it becomes the forward-walk
		// cursor once the whole window is imported.
		if pageNum == 1 && page.FirstID != "" {
			nextCursor = page.FirstID
		}

		// No checkpoint markers during a backfill: the walk moves toward
		// older activities, so a mid-window token would make a resumed run
		// skip the window's remaining older activities entirely.
		if err := s.emitPageActivities(ctx, page, "", out, progress); err != nil {
			return nextCursor, err
		}

		if !page.HasMore || page.LastID == "" {
			return nextCursor, nil
		}
		afterID = page.LastID
	}
}

// emitPageActivities filters one activities page down to importable chat
// activities and sends them to out. When pageCursor is set, the final
// importable activity carries it as the page's durable checkpoint marker —
// or a cursor-only sentinel carries it when the whole page was filtered
// out, so fully-filtered pages still advance the persisted cursor.
func (s *ComplianceImportService) emitPageActivities(ctx context.Context, page *anthropicapi.ActivitiesPage, pageCursor string, out chan<- discoveredActivity, progress *ComplianceSyncProgress) error {
	importable := make([]anthropicapi.Activity, 0, len(page.Data))
	for _, activity := range page.Data {
		if activity.Actor.Type != "user_actor" || activity.ClaudeChatID == "" {
			continue
		}
		importable = append(importable, activity)
	}

	for i, activity := range importable {
		discovered := discoveredActivity{activity: activity, activitiesCursor: "", cursorOnly: false}
		if i == len(importable)-1 {
			discovered.activitiesCursor = pageCursor
		}
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
		case out <- discovered:
			progress.ChatActivities++
		}
	}

	if len(importable) == 0 && pageCursor != "" {
		var none anthropicapi.Activity
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
		case out <- discoveredActivity{activity: none, activitiesCursor: pageCursor, cursorOnly: true}:
		}
	}
	return nil
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
func (s *ComplianceImportService) fetchChatMessages(ctx context.Context, client *anthropicapi.Client, cfg Config, chatID uuid.UUID, activity anthropicapi.Activity, afterID string, enrichChat bool, activitiesCursor string, users *connectedUserResolver, out chan<- messagePageBatch, progress *ComplianceSyncProgress) error {
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
		progress.MessagePagesFetched++

		if enrichChat && pageNum == 1 {
			if err := s.upsertMessagePageChat(ctx, cfg, chatID, page, users); err != nil {
				return err
			}
		}

		rows, err := s.buildExternalMessageRows(ctx, cfg, chatID, page, activity, users)
		if err != nil {
			return err
		}

		batch := messagePageBatch{chatID: chatID, rows: rows, lastID: page.LastID, activitiesCursor: "", cursorOnly: false}
		finalPage := !page.HasMore || page.LastID == ""
		if finalPage {
			// The chat's last message page closes out the activity; if the
			// activity closes out an activities page, tell the writer the
			// global cursor is safe to persist after this batch.
			batch.activitiesCursor = activitiesCursor
		}

		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
		case out <- batch:
		}

		if finalPage {
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
