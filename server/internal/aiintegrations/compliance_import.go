package aiintegrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/metering"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	// anthropicComplianceActivityCreated is the activity recorded when a user
	// starts a chat. It is the only chat activity the import reads: the feed
	// has no message-level activity, and claude_chat_updated covers metadata
	// edits (name, model), not new messages.
	anthropicComplianceActivityCreated = "claude_chat_created"

	anthropicComplianceSourceWeb     = "claude-chat-web"
	anthropicComplianceSourceDesktop = "claude"

	// anthropicCompliancePageLimit is the messages endpoint's maximum page.
	anthropicCompliancePageLimit = 1000
	// anthropicComplianceActivityPageLimit keeps activity pages small so the
	// fetch stage can start on a chat before the window is fully listed.
	anthropicComplianceActivityPageLimit = 100
	// anthropicComplianceChatPageLimit is the chat list's default page size;
	// one page fans out into up to that many message fetches.
	anthropicComplianceChatPageLimit  = 100
	anthropicComplianceChatBufferSize = 2 * anthropicComplianceChatPageLimit
	maxInlineExternalContentSize      = 128 * 1024

	// anthropicComplianceMessagePageBufferSize bounds how many fetched
	// message pages can be queued for writing. Each page holds up to
	// anthropicCompliancePageLimit rows with potentially large raw content,
	// so the buffer is kept small.
	anthropicComplianceMessagePageBufferSize = 2

	// anthropicComplianceActivityIndexLag keeps the activity window's upper
	// bound behind the run's end time. Anthropic documents activities as
	// queryable within one minute of occurring; a bound closer to the present
	// than that permanently drops late-indexed activities.
	anthropicComplianceActivityIndexLag = time.Minute

	// anthropicComplianceActivityWindowOverlap is how far each activity
	// window reaches back past the previous run's watermark. Consecutive
	// windows overlap rather than tile so an activity indexed after its
	// window closed is still seen; replaying an activity only re-upserts an
	// existing chat and fetches messages past its cursor.
	anthropicComplianceActivityWindowOverlap = 5 * time.Minute

	// anthropicComplianceChatCursorPrefix namespaces the chat-list cursor in
	// the schedule's last_cursor_id. The schedule once stored an activity
	// feed token there; a stored value without this prefix is that older
	// token, which the chat list would reject, and is treated as no cursor
	// so the run restarts from the bounded first-sync window.
	anthropicComplianceChatCursorPrefix = "chats:"
)

// chatClient is the identity of the client that captured a chat's messages,
// stamped on every imported message row.
type chatClient struct {
	source    string
	userAgent string
	ipAddress string
}

// discoveredChat is one chat the run must import or bring up to date. The
// activity feed yields chats as they are created, with the actor's client
// identity; the chat list yields chats whose updated_at moved past the
// stored cursor, with no client identity.
type discoveredChat struct {
	externalChatID string
	// title is the chat name when the discovery carried one; empty leaves the
	// stored title untouched until the first message page supplies it.
	title     string
	createdAt time.Time
	updatedAt time.Time
	// userEmail and externalUserID identify the chat owner when the discovery
	// carried them.
	userEmail      string
	externalUserID string
	// client is the capturing client's identity. hasClient is false for list
	// discoveries, which inherit it from the chat's stored messages.
	client    chatClient
	hasClient bool
	// chatsCursor is set on the final importable chat of a chat-list page:
	// once that chat's messages are durably written, the whole page is
	// imported and the cursor may be persisted as the schedule cursor.
	chatsCursor string
	// cursorOnly marks a sentinel for a page with no importable chats. It
	// carries just the page's cursor; the import stage forwards it to the
	// writer without fetching anything so the cursor still advances past
	// pages holding only deleted chats.
	cursorOnly bool
}

// messagePageBatch is one fetched page of chat messages ready to write.
type messagePageBatch struct {
	chatID uuid.UUID
	rows   []chat.ExternalMessageWrite
	// lastID is the page's pagination token; it advances the per-chat
	// message cursor only after the page's rows are durably written.
	lastID string
	// chatsCursor, when set, marks this batch as the last one produced by a
	// chat-list page; the writer persists it as the schedule cursor once the
	// batch is durably written, so a failed run resumes from the last
	// completed list page instead of the cursor stored by the previous
	// successful sync.
	chatsCursor string
	// cursorOnly marks a sentinel batch that carries just a chat-list cursor
	// for a page with nothing importable; there is nothing to write.
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

// SyncAnthropicCompliance imports new Claude chats and brings imported chats
// up to date with their new messages. It reads two feeds in order:
//
//   - The activity feed, window-polled for claude_chat_created between the
//     schedule watermark and endTime. A created activity carries the actor's
//     user agent, which classifies the chat as Claude web or desktop, and ip
//     address; both are stamped on its messages, so a new chat is imported
//     from here first.
//   - The chat list ordered by updated_at, walked forward from the stored
//     cursor. It is the only feed that reports a chat receiving new messages:
//     the activity feed has no message-level activity. Each chat it yields
//     has its messages fetched past the per-chat message cursor.
//
// It returns the chat-list cursor reached by this run, in its stored form.
// Callers must persist it on success so the next run resumes from it; the
// activity window needs no cursor because the watermark the caller records
// is the next window's lower bound. The chat-list cursor is also persisted
// incrementally, after each list page whose chats are fully written, so
// retries of a failed or timed-out run resume from the last completed page
// rather than repaying the whole discovery cost.
//
// The sync is a three-stage pipeline: a discovery goroutine streams chats
// from both feeds page by page, a fetch goroutine resolves each chat and
// pages its messages into row batches, and a writer goroutine persists the
// batches. This bounds memory to the channel buffers and overlaps API paging
// with the batch inserts.
//
// On failure the returned error is a SyncError that accumulates every
// stage's failure alongside the progress the run made, so one report tells
// the whole story instead of only the first error to win the race.
func (s *ComplianceImportService) SyncAnthropicCompliance(ctx context.Context, cfg Config, endTime time.Time) (string, error) {
	if cfg.Provider != ProviderAnthropicCompliance {
		return "", fmt.Errorf("unsupported ai integration provider for compliance import: %s", cfg.Provider)
	}
	if cfg.ExternalOrganizationID == nil {
		return "", fmt.Errorf("external_organization_id is required for anthropic_compliance")
	}

	client := anthropicapi.New(s.guardianPolicy, anthropicapi.WithAPIKey(cfg.APIKey))
	chatsCursor := chatsCursorFromStored(cfg.LastCursor)

	g, gctx := errgroup.WithContext(ctx)
	discovered := make(chan discoveredChat, anthropicComplianceChatBufferSize)
	messagePages := make(chan messagePageBatch, anthropicComplianceMessagePageBufferSize)

	// Each stage writes only its own progress fields and error variable;
	// everything is read after g.Wait, which establishes the happens-before.
	progress := &ComplianceSyncProgress{
		FirstSync:           chatsCursor == "",
		ActivityPages:       0,
		ChatActivities:      0,
		ChatListPages:       0,
		ChatsListed:         0,
		ChatsUnavailable:    0,
		ChatsImported:       0,
		MessagePagesFetched: 0,
		MessagePagesWritten: 0,
		CursorReached:       "",
		CursorPersisted:     "",
	}
	var nextCursor string
	var discoverErr, importErr, writeErr error

	g.Go(func() error {
		defer close(discovered)
		if discoverErr = s.streamCreatedChats(gctx, client, cfg, chatsCursor == "", endTime, discovered, progress); discoverErr != nil {
			return discoverErr
		}
		nextCursor, discoverErr = s.streamUpdatedChats(gctx, client, cfg, chatsCursor, discovered, progress)
		return discoverErr
	})

	g.Go(func() error {
		defer close(messagePages)
		importErr = s.importDiscoveredChats(gctx, client, cfg, discovered, messagePages, progress)
		return importErr
	})

	g.Go(func() error {
		writeErr = s.writeMessagePages(gctx, cfg, messagePages, progress)
		return writeErr
	})

	if err := g.Wait(); err != nil {
		progress.CursorReached = storedChatsCursor(nextCursor)
		return "", newSyncError("sync anthropic compliance", *progress,
			SyncStageError{Stage: "discover_chats", Err: discoverErr},
			SyncStageError{Stage: "import_chats", Err: importErr},
			SyncStageError{Stage: "write_messages", Err: writeErr},
		)
	}
	return storedChatsCursor(nextCursor), nil
}

// chatsCursorFromStored returns the chat-list cursor held in the schedule's
// stored cursor, or "" when nothing is stored or the stored value is the
// activity feed token the schedule kept before it walked the chat list.
func chatsCursorFromStored(stored string) string {
	cursor, ok := strings.CutPrefix(stored, anthropicComplianceChatCursorPrefix)
	if !ok {
		return ""
	}
	return cursor
}

// storedChatsCursor namespaces a chat-list cursor for the schedule's stored
// cursor. An empty cursor stays empty so nothing is stored.
func storedChatsCursor(cursor string) string {
	if cursor == "" {
		return ""
	}
	return anthropicComplianceChatCursorPrefix + cursor
}

// streamCreatedChats window-polls the activity feed for chats created since
// the previous run and sends them to out. The window runs from the schedule
// watermark, less an overlap so late-indexed activities are not lost, to
// endTime less the feed's indexing lag; a first sync reaches back the
// initial lookback instead. Activities are newest first, so the walk pages
// OLDER via after_id until the window is exhausted. Nothing is checkpointed:
// the watermark the caller records on success is the next window's start,
// and a failed run re-polls the window.
func (s *ComplianceImportService) streamCreatedChats(ctx context.Context, client *anthropicapi.Client, cfg Config, firstSync bool, endTime time.Time, out chan<- discoveredChat, progress *ComplianceSyncProgress) error {
	since := cfg.PollWatermarkAt.Add(-anthropicComplianceActivityWindowOverlap)
	if firstSync {
		since = cfg.PollWatermarkAt.Add(-initialUsagePollLookback)
	}
	until := endTime.Add(-anthropicComplianceActivityIndexLag)
	if !until.After(since) {
		return nil
	}

	afterID := ""
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "activity_discovery", pageNum)
		page, err := client.ListActivities(ctx, anthropicapi.ListActivitiesParams{
			ActivityTypes:   []string{anthropicComplianceActivityCreated},
			OrganizationIDs: []string{*cfg.ExternalOrganizationID},
			CreatedAtGTE:    since,
			CreatedAtLT:     until,
			AfterID:         afterID,
			BeforeID:        "",
			Limit:           anthropicComplianceActivityPageLimit,
		})
		if err != nil {
			return fmt.Errorf("list anthropic compliance activities: %w", err)
		}
		progress.ActivityPages++

		for _, activity := range page.Data {
			if activity.Actor.Type != "user_actor" || activity.ClaudeChatID == "" {
				continue
			}
			createdAt, err := activity.CreatedAtTime()
			if err != nil {
				return fmt.Errorf("parse anthropic compliance activity timestamp: %w", err)
			}

			found := discoveredChat{
				externalChatID: activity.ClaudeChatID,
				title:          "",
				createdAt:      createdAt,
				updatedAt:      createdAt,
				userEmail:      activity.Actor.EmailAddress,
				externalUserID: activity.Actor.UserID,
				client: chatClient{
					source:    complianceSourceFromUserAgent(activity.Actor.UserAgent),
					userAgent: activity.Actor.UserAgent,
					ipAddress: activity.Actor.IPAddress,
				},
				hasClient:   true,
				chatsCursor: "",
				cursorOnly:  false,
			}
			select {
			case <-ctx.Done():
				return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
			case out <- found:
				progress.ChatActivities++
			}
		}

		if !page.HasMore || page.LastID == "" {
			return nil
		}
		afterID = page.LastID
	}
}

// streamUpdatedChats walks the org-wide chat list ordered by updated_at
// forward from cursor and sends every chat it returns to out. A chat's
// updated_at moves when it receives a new message, is moved into or out of a
// project, or is deleted in claude.ai, so the walk returns new chats and
// chats with new messages alike; renames are not guaranteed to surface. A
// first sync has no cursor and bounds the first page with updated_at.gte
// instead; later pages are positioned by after_id, which already sits past
// the bound. It returns the newest last_id reached.
//
// The list is ascending, so each page's last_id is a safe resume point as
// soon as the page's chats are written. The final importable chat of each
// page carries it for the writer stage, or a cursor-only sentinel does when
// the page held nothing importable.
func (s *ComplianceImportService) streamUpdatedChats(ctx context.Context, client *anthropicapi.Client, cfg Config, cursor string, out chan<- discoveredChat, progress *ComplianceSyncProgress) (string, error) {
	afterID := cursor
	nextCursor := cursor
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "chat_discovery", pageNum)
		var updatedAtGTE time.Time
		if afterID == "" {
			updatedAtGTE = cfg.PollWatermarkAt.Add(-initialUsagePollLookback)
		}
		page, err := client.ListChats(ctx, anthropicapi.ListChatsParams{
			OrganizationIDs: []string{*cfg.ExternalOrganizationID},
			OrderBy:         anthropicapi.ChatOrderByUpdatedAt,
			UpdatedAtGTE:    updatedAtGTE,
			AfterID:         afterID,
			Limit:           anthropicComplianceChatPageLimit,
		})
		if err != nil {
			return nextCursor, fmt.Errorf("list anthropic compliance chats: %w", err)
		}
		progress.ChatListPages++

		if err := s.emitPageChats(ctx, page, page.LastID, out, progress); err != nil {
			return nextCursor, err
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

// emitPageChats sends a chat-list page's importable chats to out. Chats
// deleted in claude.ai are skipped: they are still listed, with deleted_at
// set, but their messages come back without content. When pageCursor is set
// the final importable chat carries it as the page's checkpoint, or a
// cursor-only sentinel does when the whole page was skipped, so such pages
// still advance the persisted cursor.
func (s *ComplianceImportService) emitPageChats(ctx context.Context, page *anthropicapi.ChatsPage, pageCursor string, out chan<- discoveredChat, progress *ComplianceSyncProgress) error {
	importable := make([]discoveredChat, 0, len(page.Data))
	for _, summary := range page.Data {
		if summary.ID == "" || summary.DeletedAt != nil {
			continue
		}
		createdAt := parseTimeOrDefault(summary.CreatedAt, time.Time{})
		importable = append(importable, discoveredChat{
			externalChatID: summary.ID,
			title:          conv.StripNUL(summary.Name),
			createdAt:      createdAt,
			updatedAt:      parseTimeOrDefault(summary.UpdatedAt, createdAt),
			userEmail:      summary.User.EmailAddress,
			externalUserID: summary.User.ID,
			client:         chatClient{source: "", userAgent: "", ipAddress: ""},
			hasClient:      false,
			chatsCursor:    "",
			cursorOnly:     false,
		})
	}

	for i := range importable {
		if i == len(importable)-1 {
			importable[i].chatsCursor = pageCursor
		}
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
		case out <- importable[i]:
			progress.ChatsListed++
		}
	}

	if len(importable) == 0 && pageCursor != "" {
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
		case out <- discoveredChat{
			externalChatID: "",
			title:          "",
			createdAt:      time.Time{},
			updatedAt:      time.Time{},
			userEmail:      "",
			externalUserID: "",
			client:         chatClient{source: "", userAgent: "", ipAddress: ""},
			hasClient:      false,
			chatsCursor:    pageCursor,
			cursorOnly:     true,
		}:
		}
	}
	return nil
}

// importDiscoveredChats consumes discovered chats, upserts the chat rows, and
// pages each chat's new messages into row batches for the writer stage. A
// chat may arrive more than once per run, from both feeds or from several
// list pages; chat upserts are idempotent and each visit fetches only the
// messages past the chat's persisted cursor.
func (s *ComplianceImportService) importDiscoveredChats(ctx context.Context, client *anthropicapi.Client, cfg Config, in <-chan discoveredChat, out chan<- messagePageBatch, progress *ComplianceSyncProgress) error {
	users := newConnectedUserResolver(s.db, cfg.OrganizationID)
	// clientsByChat remembers the client identity resolved for each chat
	// this run, keyed by external chat id. A list revisit of a chat can
	// arrive before the writer has committed the feed's rows for it, when
	// the stored-message lookup would still find nothing and fall back to
	// web; the feed discovery's identity wins instead.
	clientsByChat := map[string]chatClient{}
	for found := range in {
		if found.cursorOnly {
			// A page with nothing importable has no chats to import; forward
			// its cursor straight to the writer so it still becomes durable
			// after all previously discovered work is written.
			select {
			case <-ctx.Done():
				return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
			case out <- messagePageBatch{chatID: uuid.Nil, rows: nil, lastID: "", chatsCursor: found.chatsCursor, cursorOnly: true}:
			}
			continue
		}

		s.heartbeat(ctx, "chat_import", progress.ChatsImported)
		chatID, messagesCursor, err := s.upsertDiscoveredChat(ctx, cfg, found, users)
		if err != nil {
			return err
		}
		progress.ChatsImported++

		capturedBy, known := clientsByChat[found.externalChatID]
		if found.hasClient {
			capturedBy, known = found.client, true
		}
		if !known {
			capturedBy, err = s.storedChatClient(ctx, cfg, chatID)
			if err != nil {
				return err
			}
		}
		clientsByChat[found.externalChatID] = capturedBy

		if err := s.fetchChatMessages(ctx, client, cfg, chatID, found.externalChatID, capturedBy, messagesCursor, found.chatsCursor, users, out, progress); err != nil {
			return err
		}
	}
	return nil
}

// writeMessagePages persists fetched message pages. It must remain a single
// goroutine: the per-chat message cursor may only advance after that page's
// rows are durably written, and pages of one chat must be written in feed
// order. When a batch closes out a chat-list page, the schedule cursor is
// advanced so retries resume from the last completed page.
func (s *ComplianceImportService) writeMessagePages(ctx context.Context, cfg Config, in <-chan messagePageBatch, progress *ComplianceSyncProgress) error {
	for batch := range in {
		if !batch.cursorOnly {
			s.heartbeat(ctx, "message_write", progress.MessagePagesWritten+1)

			if _, err := s.writer.WriteExternal(ctx, cfg.ProjectID, batch.rows); err != nil {
				return fmt.Errorf("write anthropic compliance chat messages: %w", err)
			}

			if batch.lastID != "" {
				if err := chatrepo.New(s.db).UpdateAIIntegrationConfigChatCursor(ctx, chatrepo.UpdateAIIntegrationConfigChatCursorParams{
					LastCursorID: conv.ToPGText(batch.lastID),
					ChatID:       batch.chatID,
					ProjectID:    cfg.ProjectID,
				}); err != nil {
					return fmt.Errorf("record anthropic compliance chat cursor: %w", err)
				}
			}
			progress.MessagePagesWritten++
		}

		// Batches arrive in list order, so once the last batch of a list
		// page is written, everything discovered up to that page's cursor is
		// durable and the cursor is a safe resume point.
		if batch.chatsCursor != "" {
			if err := repo.New(s.db).AdvanceUsagePollCursor(ctx, repo.AdvanceUsagePollCursorParams{
				LastCursorID:          conv.ToPGText(storedChatsCursor(batch.chatsCursor)),
				AiIntegrationConfigID: cfg.ID,
				Schedule:              ScheduleAnthropicCompliance,
			}); err != nil {
				return fmt.Errorf("advance anthropic compliance chats cursor: %w", err)
			}
			progress.CursorPersisted = storedChatsCursor(batch.chatsCursor)
		}
	}
	return nil
}

// upsertDiscoveredChat resolves the chat row for a discovered chat and
// returns its persisted message pagination cursor alongside the chat id.
func (s *ComplianceImportService) upsertDiscoveredChat(ctx context.Context, cfg Config, found discoveredChat, users *connectedUserResolver) (uuid.UUID, string, error) {
	createdAt := found.createdAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := found.updatedAt
	if updatedAt.Before(createdAt) {
		updatedAt = createdAt
	}

	userID, err := users.resolve(ctx, found.userEmail)
	if err != nil {
		return uuid.Nil, "", err
	}
	upserted, err := chatrepo.New(s.db).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             uuid.New(),
		ProjectID:      cfg.ProjectID,
		OrganizationID: cfg.OrganizationID,
		// NULL when unresolved so the upsert's COALESCE preserves a user
		// resolved by an earlier discovery or the message-page enrichment.
		UserID:         conv.ToPGTextEmpty(userID),
		ExternalUserID: conv.ToPGTextEmpty(found.externalUserID),
		ExternalChatID: conv.ToPGText(found.externalChatID),
		// NULL when the discovery carried no title so the upsert's COALESCE
		// keeps the title set from the chat's first message page.
		Title:     conv.ToPGTextEmpty(found.title),
		CreatedAt: conv.ToPGTimestamptz(createdAt),
		UpdatedAt: conv.ToPGTimestamptz(updatedAt),
		// Feed titles are authoritative: newest non-null title wins.
		PreferStoredTitle: false,
	})
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("upsert anthropic compliance chat: %w", err)
	}
	chatID := upserted.ID
	messagesCursor, err := chatrepo.New(s.db).LinkAIIntegrationConfigChat(ctx, chatrepo.LinkAIIntegrationConfigChatParams{
		AiIntegrationConfigID: cfg.ID,
		ChatID:                chatID,
		ProjectID:             cfg.ProjectID,
	})
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("link anthropic compliance chat: %w", err)
	}
	return chatID, messagesCursor.String, nil
}

// storedChatClient returns the client identity stamped on the chat's newest
// imported message, so the messages a chat-list visit adds keep the source
// its created activity established. A chat with no imported messages yet is
// attributed to Claude web, the surface with no distinguishing user agent.
func (s *ComplianceImportService) storedChatClient(ctx context.Context, cfg Config, chatID uuid.UUID) (chatClient, error) {
	row, err := chatrepo.New(s.db).GetLatestExternalChatMessageClient(ctx, chatrepo.GetLatestExternalChatMessageClientParams{
		ChatID:    chatID,
		ProjectID: cfg.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return chatClient{source: anthropicComplianceSourceWeb, userAgent: "", ipAddress: ""}, nil
	case err != nil:
		return chatClient{source: "", userAgent: "", ipAddress: ""}, fmt.Errorf("load anthropic compliance chat client: %w", err)
	}

	source := row.Source.String
	if source == "" {
		source = anthropicComplianceSourceWeb
	}
	return chatClient{source: source, userAgent: row.UserAgent.String, ipAddress: row.IpAddress.String}, nil
}

// fetchChatMessages pages through a chat's messages starting after the
// chat's persisted cursor and sends each page's rows to out for the writer
// stage to persist. The writer advances the per-chat cursor after every
// successfully written page so a failed run resumes at the last completed
// page instead of re-importing the whole chat.
//
// The first page's chat-level metadata (title, owner, timestamps) is
// upserted onto the chat row on every visit: the feed's title is
// authoritative and a rename only surfaces through a later visit.
func (s *ComplianceImportService) fetchChatMessages(ctx context.Context, client *anthropicapi.Client, cfg Config, chatID uuid.UUID, externalChatID string, capturedBy chatClient, afterID string, chatsCursor string, users *connectedUserResolver, out chan<- messagePageBatch, progress *ComplianceSyncProgress) error {
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "message_import", pageNum)
		page, err := client.GetChatMessages(ctx, anthropicapi.GetChatMessagesParams{
			ClaudeChatID: externalChatID,
			AfterID:      afterID,
			Limit:        anthropicCompliancePageLimit,
		})
		if err != nil {
			if httpErr, ok := errors.AsType[*anthropicapi.HTTPError](err); ok && httpErr.StatusCode == http.StatusNotFound {
				return s.skipUnretrievableChat(ctx, cfg, chatID, externalChatID, chatsCursor, out, progress)
			}
			return fmt.Errorf("get anthropic compliance chat messages: %w", err)
		}
		progress.MessagePagesFetched++

		if pageNum == 1 {
			if err := s.upsertMessagePageChat(ctx, cfg, chatID, page, users); err != nil {
				return err
			}
		}

		rows, err := s.buildExternalMessageRows(ctx, cfg, chatID, page, capturedBy, users)
		if err != nil {
			return err
		}

		batch := messagePageBatch{chatID: chatID, rows: rows, lastID: page.LastID, chatsCursor: "", cursorOnly: false}
		finalPage := !page.HasMore || page.LastID == ""
		if finalPage {
			// The chat's last message page closes out the discovery; if the
			// discovery closes out a chat-list page, tell the writer the
			// schedule cursor is safe to persist after this batch.
			batch.chatsCursor = chatsCursor
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

// skipUnretrievableChat records a chat whose messages Anthropic no longer
// serves and lets the run continue. The chats endpoint answers 404 for a chat
// hard-deleted through the Compliance API or by the organization's retention
// policy, while the activity feed and the chat list can still name it.
// Failing the run on it would stall the whole integration on one chat, and
// the poller counts a 404 as a provider rejection toward auto-pause. The chat
// row already upserted for it stays, with no messages. When the chat closed
// out a chat-list page, the page's cursor is still forwarded to the writer.
func (s *ComplianceImportService) skipUnretrievableChat(ctx context.Context, cfg Config, chatID uuid.UUID, externalChatID string, chatsCursor string, out chan<- messagePageBatch, progress *ComplianceSyncProgress) error {
	s.logger.WarnContext(ctx, "anthropic compliance chat content is not retrievable; skipping chat",
		attr.SlogChatID(chatID.String()),
		attr.SlogChatExternalID(externalChatID),
		attr.SlogAIIntegrationConfigID(cfg.ID.String()),
		attr.SlogHTTPResponseStatusCode(http.StatusNotFound),
	)
	progress.ChatsUnavailable++

	if chatsCursor == "" {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
	case out <- messagePageBatch{chatID: uuid.Nil, rows: nil, lastID: "", chatsCursor: chatsCursor, cursorOnly: true}:
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
	resolved, err := chatrepo.New(s.db).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             chatID,
		ProjectID:      cfg.ProjectID,
		OrganizationID: cfg.OrganizationID,
		// NULL when unresolved so the upsert's COALESCE preserves a user
		// resolved from a prior sync.
		UserID:         conv.ToPGTextEmpty(userID),
		ExternalUserID: conv.ToPGTextEmpty(page.User.ID),
		ExternalChatID: conv.ToPGText(page.ID),
		Title:          conv.ToPGText(conv.StripNUL(page.Name)),
		CreatedAt:      conv.ToPGTimestamptz(createdAt),
		UpdatedAt:      conv.ToPGTimestamptz(updatedAt),
		// Feed titles are authoritative: newest non-null title wins.
		PreferStoredTitle: false,
	})
	if err != nil {
		return fmt.Errorf("upsert anthropic compliance chat metadata: %w", err)
	}
	if resolved.ID != chatID {
		s.logger.WarnContext(ctx, "anthropic compliance chat resolved to different id",
			attr.SlogChatID(resolved.ID.String()),
			attr.SlogAIIntegrationConfigID(cfg.ID.String()),
		)
	}
	return nil
}

func (s *ComplianceImportService) buildExternalMessageRows(ctx context.Context, cfg Config, chatID uuid.UUID, page *anthropicapi.ChatMessagesPage, capturedBy chatClient, users *connectedUserResolver) ([]chat.ExternalMessageWrite, error) {
	rows := make([]chat.ExternalMessageWrite, 0, len(page.Messages))
	userID, err := users.resolve(ctx, page.User.EmailAddress)
	if err != nil {
		return nil, err
	}
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
			return nil, fmt.Errorf("parse anthropic compliance message timestamp: %w", err)
		}

		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}

		content := renderComplianceContent(msg.Content)
		var contentRaw []byte
		if len(msg.Content) > 0 && len(msg.Content) <= maxInlineExternalContentSize {
			contentRaw = msg.Content
		}

		rows = append(rows, chat.ExternalMessageWrite{
			Params: chatrepo.CreateExternalChatMessageParams{
				ID:                uuid.Nil,
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
				UserAgent:         conv.ToPGTextEmpty(capturedBy.userAgent),
				IpAddress:         conv.ToPGTextEmpty(capturedBy.ipAddress),
				Source:            conv.ToPGText(capturedBy.source),
				ContentHash:       nil,
				Generation:        0,
				CreatedAt:         conv.ToPGTimestamptz(createdAt),
			},
			BillingUserID:  userID,
			WorkloadSource: metering.WorkloadSourceImport,
			UserEmail:      page.User.EmailAddress,
			Provider:       anthropicAnalyticsProviderTag,
			HookHostname:   "",
			AccountType:    complianceAccountTypeTeam,
			BillingMode:    cfg.BillingMode,
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
// duration of one sync run. It is not safe for concurrent use: each import
// run constructs its own resolver and must call it from a single goroutine
// (the Anthropic import's fetch goroutine, the ChatGPT import's ProcessPage
// consumer).
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
		return "", fmt.Errorf("hydrate compliance connected user: %w", err)
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
