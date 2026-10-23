package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	chatSearchSortNewestFirst = "newest_first"
	chatSearchSortOldestFirst = "oldest_first"
)

const (
	// chatSearchMaxInterval bounds the from/to window a single search may scan.
	chatSearchMaxInterval = 31 * 24 * time.Hour
	// chatSearchTimeout bounds each search or read, since message text has no
	// search index and a broad window over a busy project can be slow.
	chatSearchTimeout = 10 * time.Second
	// chatSearchDefaultLimit and chatSearchMaxLimit bound hits per page.
	chatSearchDefaultLimit = 10
	chatSearchMaxLimit     = 20
	// chatSearchMaxChatIDs bounds the chat_ids filter.
	chatSearchMaxChatIDs = 50
	// chatSearchSnippetRunes is the search snippet length around the match.
	chatSearchSnippetRunes = 1200
	// chatReadDefaultContext and chatReadMaxContext bound messages on each
	// side of the read anchor.
	chatReadDefaultContext = 3
	chatReadMaxContext     = 5
	// chatReadTextRunes caps each message's text in a read.
	chatReadTextRunes = 4000
)

var (
	errChatSearchInvalid = errors.New("invalid chat search request")
	errChatSearchTimeout = errors.New("chat search timed out")
)

// ChatSearchService searches and reads captured chat messages under an
// explicit transcript grant. It never inherits the metadata listing's
// org-admin bypass.
type ChatSearchService struct {
	db         *pgxpool.Pool
	engine     *authz.Engine
	audit      *audit.Logger
	budget     OperationBudget
	references *subjectReferenceCodec
	scanner    *gitleaks.Scanner
}

// NewChatSearchService composes the external-only transcript tools. Missing
// dependencies leave stable unavailable tools registered on the server.
func NewChatSearchService(db *pgxpool.Pool, engine *authz.Engine, auditLogger *audit.Logger, budget OperationBudget, keyMaterial string) *ChatSearchService {
	codec, err := newSubjectReferenceCodec(keyMaterial)
	if db == nil || engine == nil || auditLogger == nil || !budget.valid() || err != nil {
		return nil
	}
	return &ChatSearchService{db: db, engine: engine, audit: auditLogger, budget: budget, references: codec, scanner: gitleaks.NewScanner()}
}

func (s *ChatSearchService) valid() bool {
	return s != nil && s.db != nil && s.engine != nil && s.audit != nil && s.budget.valid() && s.references != nil && s.scanner != nil
}

// SearchChatsInput selects an explicit project and date interval.
type SearchChatsInput struct {
	ProjectID string `json:"project_id,omitempty"`

	ProjectSlug string `json:"project_slug,omitempty"`

	From string `json:"from"`

	To string `json:"to"`

	// Query is a literal case-insensitive substring; empty browses messages.
	Query string `json:"query,omitempty"`

	SortOrder string `json:"sort_order,omitempty"`

	OwnerUserID string `json:"owner_user_id,omitempty"`

	// ParticipantID matches a message's user ID or a recorded participant's
	// user ID or provider user ID.
	ParticipantID string `json:"participant_id,omitempty"`

	// ParticipantProvider restricts ParticipantID to recorded participants of
	// this provider.
	ParticipantProvider string `json:"participant_provider,omitempty"`

	ChatIDs []string `json:"chat_ids,omitempty"`

	Source string `json:"source,omitempty"`

	Limit int `json:"limit,omitempty"`

	Cursor string `json:"cursor,omitempty"`
}

// ChatMessageHit is one matching captured message.
type ChatMessageHit struct {
	ChatID string `json:"chat_id"`

	ChatTitle string `json:"chat_title"`

	OwnerUserID string `json:"owner_user_id,omitempty"`

	MessageID string `json:"message_id"`

	Sequence int64 `json:"sequence"`

	CreatedAt time.Time `json:"created_at"`

	Role string `json:"role"`

	Source string `json:"source,omitempty"`

	// UserID is the message's recorded internal user, when captured.
	UserID string `json:"user_id,omitempty"`

	// Generation distinguishes stored versions of a compacted or edited
	// conversation.
	Generation int32 `json:"generation"`

	// Text is a redacted snippet around the match.
	Text string `json:"text"`

	// Truncated reports a shortened snippet or a field too large to return.
	Truncated bool `json:"truncated"`
}

// SearchChatsOutput is one page of hits.
type SearchChatsOutput struct {
	Hits []ChatMessageHit `json:"hits"`

	// NextCursor continues the search; a page may be short or empty and still
	// have one.
	NextCursor string `json:"next_cursor,omitempty"`
}

// chatSearchCursor is the keyset position of the last row a page consumed.
type chatSearchCursor struct {
	CreatedAt time.Time `json:"created_at"`

	ID uuid.UUID `json:"id"`
}

// chatSearchScope binds a cursor to the normalized filters that produced it.
type chatSearchScope struct {
	ProjectID           string   `json:"project_id"`
	From                string   `json:"from"`
	To                  string   `json:"to"`
	Query               string   `json:"query"`
	SortOrder           string   `json:"sort_order"`
	OwnerUserID         string   `json:"owner_user_id"`
	ParticipantID       string   `json:"participant_id"`
	ParticipantProvider string   `json:"participant_provider"`
	ChatIDs             []string `json:"chat_ids"`
	Source              string   `json:"source"`
}

func (s *ChatSearchService) authorize(ctx context.Context, principal Principal, projectID, projectSlug string) (ResolvedProject, error) {
	var zero ResolvedProject
	if !s.valid() {
		return zero, ErrUnavailable
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if principal.surface() != SurfacePlatformMCP || principal.UserID == "" || principal.OrganizationID == "" || !ok || authCtx == nil || authCtx.UserID != principal.UserID || authCtx.ActiveOrganizationID != principal.OrganizationID || authCtx.SupportOrganizationID != "" || authCtx.APIKeyID != "" {
		return zero, ErrForbidden
	}
	if projectID == "" && projectSlug == "" {
		return zero, errChatSearchInvalid
	}
	enforce, err := s.engine.ShouldEnforce(ctx)
	if err != nil || !enforce {
		return zero, ErrUnavailable
	}
	if _, ok := authz.GrantsFromContext(ctx); !ok {
		return zero, ErrUnavailable
	}
	// Transcript grants are unrestricted. Refuse before project resolution so
	// a caller without one cannot probe project existence through error codes.
	if err := s.engine.Require(ctx, authz.ChatReadCheck(principal.OrganizationID)); err != nil {
		return zero, &ExternalAuthorizationError{RequiredScope: string(authz.ScopeChatRead), RequestAccessURL: "", cause: err}
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return zero, err
	}
	project, err := (postgresRiskProjectResolver{queries: platformrepo.New(s.db)}).Resolve(ctx, principal.OrganizationID, projectID, projectSlug)
	if err != nil {
		return zero, err
	}
	if err := s.engine.Require(ctx, authz.ChatReadCheck(project.ID.String())); err != nil {
		return zero, &ExternalAuthorizationError{RequiredScope: string(authz.ScopeChatRead), RequestAccessURL: "", cause: err}
	}
	return project, nil
}

// Search returns one keyset page of matching messages. SQL matches raw stored
// text; a hit whose redacted text no longer shows the query is dropped, but
// the page still advances past it so hidden matches never stall a cursor.
func (s *ChatSearchService) Search(ctx context.Context, principal Principal, input SearchChatsInput) (SearchChatsOutput, error) {
	var zero SearchChatsOutput
	if input.SortOrder == "" {
		input.SortOrder = chatSearchSortNewestFirst
	}
	limit := input.Limit
	if limit == 0 {
		limit = chatSearchDefaultLimit
	}
	from, fromErr := time.Parse(time.RFC3339Nano, input.From)
	to, toErr := time.Parse(time.RFC3339Nano, input.To)
	if fromErr != nil || toErr != nil || !to.After(from) || to.Sub(from) > chatSearchMaxInterval ||
		(input.SortOrder != chatSearchSortNewestFirst && input.SortOrder != chatSearchSortOldestFirst) ||
		!validChatSearchFilter(input.Query, 256) || !validChatSearchFilter(input.OwnerUserID, 128) || !validChatSearchFilter(input.Source, 128) ||
		!validChatSearchFilter(input.ParticipantID, 128) || !validChatSearchFilter(input.ParticipantProvider, 128) ||
		(input.ParticipantProvider != "" && input.ParticipantID == "") ||
		limit < 1 || limit > chatSearchMaxLimit || len(input.ChatIDs) > chatSearchMaxChatIDs || len(input.Cursor) > 4096 {
		return zero, errChatSearchInvalid
	}
	chatIDs := make([]uuid.UUID, 0, len(input.ChatIDs))
	for _, value := range input.ChatIDs {
		id, err := uuid.Parse(value)
		if err != nil {
			return zero, errChatSearchInvalid
		}
		chatIDs = append(chatIDs, id)
	}
	slices.SortFunc(chatIDs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	chatIDs = slices.Compact(chatIDs)

	project, err := s.authorize(ctx, principal, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, chatSearchTimeout)
	defer cancel()

	scopeIDs := make([]string, len(chatIDs))
	for i, id := range chatIDs {
		scopeIDs[i] = id.String()
	}
	encodedScope, err := json.Marshal(chatSearchScope{
		ProjectID: project.ID.String(), From: from.UTC().Format(time.RFC3339Nano), To: to.UTC().Format(time.RFC3339Nano),
		Query: input.Query, SortOrder: input.SortOrder, OwnerUserID: input.OwnerUserID, ParticipantID: input.ParticipantID,
		ParticipantProvider: input.ParticipantProvider, ChatIDs: scopeIDs, Source: input.Source,
	})
	if err != nil {
		return zero, fmt.Errorf("encode chat search scope: %w", err)
	}
	scope := queryScope("chat-search-v3", principal.UserID, principal.ConnectionID, string(encodedScope))
	now := time.Now()
	cursor := chatSearchCursor{CreatedAt: time.Time{}, ID: uuid.Nil}
	if input.Cursor != "" {
		value, err := s.references.DecodeScoped(input.Cursor, principal, subjectKindCursor, scope, now)
		if err != nil {
			return zero, err
		}
		if err := json.Unmarshal([]byte(value), &cursor); err != nil {
			return zero, ErrSubjectReferenceNotFound
		}
	}

	rows, err := platformrepo.New(s.db).SearchPlatformChatMessages(ctx, platformrepo.SearchPlatformChatMessagesParams{
		ProjectID: project.ID, OrganizationID: principal.OrganizationID,
		FromTime: conv.ToPGTimestamptz(from), ToTime: conv.ToPGTimestamptz(to),
		ChatIds: chatIDs, OwnerUserID: input.OwnerUserID, Source: input.Source, Pattern: likeContainsPattern(input.Query),
		ParticipantID: input.ParticipantID, ParticipantProvider: input.ParticipantProvider,
		HasCursor: input.Cursor != "", OldestFirst: input.SortOrder == chatSearchSortOldestFirst,
		CursorAt: conv.ToPGTimestamptz(cursor.CreatedAt), CursorID: cursor.ID,
		RowLimit: int32(limit + 1),
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return zero, errChatSearchTimeout
		}
		return zero, fmt.Errorf("search chat messages: %w", err)
	}
	more := len(rows) > limit
	rows = rows[:min(len(rows), limit)]

	output := SearchChatsOutput{Hits: []ChatMessageHit{}, NextCursor: ""}
	owners := map[uuid.UUID]string{}
	for _, row := range rows {
		text, changed, err := s.messageText(ctx, row.Content, row.ToolCalls, row.ContentRaw)
		if err != nil {
			return zero, err
		}
		title, titleChanged, err := s.redactText(ctx, row.ChatTitle)
		if err != nil {
			return zero, err
		}
		if input.Query != "" && (changed || titleChanged) && !containsFold(text, input.Query) && !containsFold(title, input.Query) {
			continue
		}
		source, _, err := s.redactText(ctx, row.Source)
		if err != nil {
			return zero, err
		}
		snippet, truncated := chatSnippet(text, input.Query, chatSearchSnippetRunes)
		output.Hits = append(output.Hits, ChatMessageHit{
			ChatID: row.ChatID.String(), ChatTitle: title, OwnerUserID: row.OwnerUserID, MessageID: row.ID.String(),
			Sequence: row.Seq, CreatedAt: row.CreatedAt.Time.UTC(), Role: row.Role, Source: source, UserID: row.UserID,
			Generation: row.Generation, Text: snippet, Truncated: truncated || row.Oversized,
		})
		owners[row.ChatID] = row.OwnerUserID
	}
	if more {
		last := rows[len(rows)-1]
		value, err := json.Marshal(chatSearchCursor{CreatedAt: last.CreatedAt.Time.UTC(), ID: last.ID})
		if err != nil {
			return zero, fmt.Errorf("encode chat search cursor: %w", err)
		}
		output.NextCursor, err = s.references.EncodeScoped(principal, subjectKindCursor, scope, string(value), now)
		if err != nil {
			return zero, fmt.Errorf("seal chat search cursor: %w", err)
		}
	}
	for id, owner := range owners {
		if err := s.recordAccess(ctx, principal, project.ID, id, owner); err != nil {
			return zero, err
		}
	}
	return output, nil
}

func validChatSearchFilter(value string, maxRunes int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxRunes && !strings.ContainsRune(value, 0)
}

// likeContainsPattern escapes LIKE wildcards so the query matches literally.
func likeContainsPattern(query string) string {
	if query == "" {
		return ""
	}
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
}

func containsFold(text, query string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(query))
}

// ReadChatInput anchors a small context window on one captured message.
type ReadChatInput struct {
	ProjectID string `json:"project_id,omitempty"`

	ProjectSlug string `json:"project_slug,omitempty"`

	ChatID string `json:"chat_id"`

	// MessageID anchors the window; empty anchors on the latest message.
	MessageID string `json:"message_id,omitempty"`

	ContextSize *int `json:"context_size,omitempty"`
}

// ChatParticipant is a participant snapshot recorded on a message.
type ChatParticipant struct {
	Provider string `json:"provider"`

	ProviderUserID string `json:"provider_user_id"`

	UserID string `json:"user_id,omitempty"`

	DisplayName string `json:"display_name,omitempty"`
}

// ChatMessage is one captured message in a read window.
type ChatMessage struct {
	MessageID string `json:"message_id"`

	Sequence int64 `json:"sequence"`

	CreatedAt time.Time `json:"created_at"`

	Role string `json:"role"`

	Source string `json:"source,omitempty"`

	UserID string `json:"user_id,omitempty"`

	// Participants holds at most five recorded participant snapshots.
	Participants []ChatParticipant `json:"participants"`

	Generation int32 `json:"generation"`

	// Text is the redacted message, including tool calls and stored payload.
	Text string `json:"text"`

	// Truncated reports shortened text or a field too large to return.
	Truncated bool `json:"truncated"`
}

// ReadChatOutput carries capture-sequence messages around the anchor.
type ReadChatOutput struct {
	ChatID string `json:"chat_id"`

	ChatTitle string `json:"chat_title"`

	OwnerUserID string `json:"owner_user_id,omitempty"`

	Messages []ChatMessage `json:"messages"`
}

// Read checks the project and chat again; search results never grant access.
func (s *ChatSearchService) Read(ctx context.Context, principal Principal, input ReadChatInput) (ReadChatOutput, error) {
	var zero ReadChatOutput
	chatID, err := uuid.Parse(input.ChatID)
	if err != nil {
		return zero, errChatSearchInvalid
	}
	var messageID uuid.NullUUID
	if input.MessageID != "" {
		id, err := uuid.Parse(input.MessageID)
		if err != nil {
			return zero, errChatSearchInvalid
		}
		messageID = uuid.NullUUID{UUID: id, Valid: true}
	}
	size := chatReadDefaultContext
	if input.ContextSize != nil {
		size = *input.ContextSize
	}
	if size < 0 || size > chatReadMaxContext {
		return zero, errChatSearchInvalid
	}
	project, err := s.authorize(ctx, principal, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, chatSearchTimeout)
	defer cancel()
	queries := platformrepo.New(s.db)
	chat, err := queries.GetPlatformChatSearchChat(ctx, platformrepo.GetPlatformChatSearchChatParams{OrganizationID: principal.OrganizationID, ChatID: chatID, ProjectID: project.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrRiskReadNotFound
	}
	if err != nil {
		return zero, fmt.Errorf("get searched chat: %w", err)
	}
	if err := s.recordAccess(ctx, principal, project.ID, chatID, chat.OwnerUserID); err != nil {
		return zero, err
	}
	rows, err := queries.ReadPlatformChatMessages(ctx, platformrepo.ReadPlatformChatMessagesParams{ProjectID: project.ID, OrganizationID: principal.OrganizationID, MessageID: messageID, ChatID: chatID, ContextSize: int32(size), FollowingLimit: int32(size + 1)})
	if err != nil {
		return zero, fmt.Errorf("read chat messages: %w", err)
	}
	if len(rows) == 0 {
		return zero, ErrRiskReadNotFound
	}
	title, _, err := s.redactText(ctx, chat.Title)
	if err != nil {
		return zero, err
	}
	output := ReadChatOutput{ChatID: chatID.String(), ChatTitle: title, OwnerUserID: chat.OwnerUserID, Messages: make([]ChatMessage, 0, len(rows))}
	for _, row := range rows {
		text, _, err := s.messageText(ctx, row.Content, row.ToolCalls, row.ContentRaw)
		if err != nil {
			return zero, err
		}
		text, truncated := chatSnippet(text, "", chatReadTextRunes)
		source, _, err := s.redactText(ctx, row.Source)
		if err != nil {
			return zero, err
		}
		participantJSON, _, err := s.redactText(ctx, string(row.Participants))
		if err != nil {
			return zero, err
		}
		participants := []ChatParticipant{}
		if err := json.Unmarshal([]byte(participantJSON), &participants); err != nil {
			return zero, fmt.Errorf("decode chat participants: %w", err)
		}
		output.Messages = append(output.Messages, ChatMessage{
			MessageID: row.ID.String(), Sequence: row.Seq, CreatedAt: row.CreatedAt.Time.UTC(), Role: row.Role, Source: source,
			UserID: row.UserID, Participants: participants, Generation: row.Generation, Text: text, Truncated: truncated || row.Oversized,
		})
	}
	return output, nil
}

func (s *ChatSearchService) recordAccess(ctx context.Context, principal Principal, projectID, chatID uuid.UUID, owner string) error {
	if err := s.audit.LogChatSessionAccess(ctx, s.db, audit.LogChatSessionAccessEvent{
		OrganizationID: principal.OrganizationID, ProjectID: projectID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), ChatSessionURN: urn.NewChatSession(chatID), OwnerUserID: owner, ActorDisplayName: nil, ActorSlug: nil, ChatTitle: "",
	}); err != nil {
		return fmt.Errorf("audit chat search access: %w", err)
	}
	return nil
}

// chatSnippet cuts already-redacted text to maxRunes, centred near the first
// match, so truncation can never split a secret before it is masked.
func chatSnippet(text, query string, maxRunes int) (string, bool) {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text, false
	}
	match := 0
	if query != "" {
		// strings.ToLower maps rune for rune, so rune offsets carry over.
		lowered := strings.ToLower(text)
		if at := strings.Index(lowered, strings.ToLower(query)); at >= 0 {
			match = utf8.RuneCountInString(lowered[:at])
		}
	}
	start := min(max(0, match-maxRunes/3), len(runes)-maxRunes)
	return string(runes[start : start+maxRunes]), true
}
