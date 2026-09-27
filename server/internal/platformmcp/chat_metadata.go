//nolint:exhaustruct // Chat metadata projections intentionally omit documented optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const (
	defaultChatListLimit = 20
	maxChatListLimit     = 50
	// maxChatListTraversal bounds how many chats one listing may page through
	// in total. A page cap alone bounds a single response; without a traversal
	// cap a caller can still walk a whole month of a project's conversations one
	// page at a time, which is the export this surface exists not to be.
	maxChatListTraversal = 500
	maxChatSourceLength  = 64
)

// chatListWindowSpec is the window policy for chat listings. A week is the
// question an administrator usually asks; a month is as far back as a bounded
// page can be walked meaningfully.
var chatListWindowSpec = windowSpec{Fallback: DiagnosticWindowLastWeek, Max: DiagnosticWindowLastMonth}

// Risk presence filters a listing may narrow to. Empty means no filter.
const (
	ChatRiskWithFindings    = "with_findings"
	ChatRiskWithoutFindings = "without_findings"
)

// ErrChatListInvalid is returned for a filter outside the closed sets, a
// malformed selector, or a source label that cannot be matched safely.
var ErrChatListInvalid = errors.New("invalid platform mcp chat list")

// chatListLimitations is stated on every page so a caller never mistakes the
// listing for a transcript reader or an export.
const chatListLimitations = "Metadata only: chat titles, messages, prompts, tool inputs and outputs are never returned; read a transcript in the dashboard. The window selects chats by activity: a chat is listed when its last message is at or after the window start and it was created at or before the window end, newest activity first. A cursor pins the window it was minted for, so later pages walk the same interval. Identities are masked and user references expire and are bound to this session and project. risk_findings_count counts live findings under enabled policies; excluded and dismissed findings are not counted. One row is returned per chat; a chat that several assistants worked in reports the assistant the listing was narrowed to, or the first, and total_matches counts assistant threads for such chats. Pages are bounded and at most 500 chats can be walked per listing. total_matches is the count at the time of this page and can change between pages."

// chatMetadataReader is the pair of chat queries the listing reads through, so
// unit tests can model the read without a database.
type chatMetadataReader interface {
	ListChats(ctx context.Context, arg chatrepo.ListChatsParams) ([]chatrepo.ListChatsRow, error)
	CountChats(ctx context.Context, arg chatrepo.CountChatsParams) (int64, error)
}

// ChatMetadataService serves list_chats: one project's conversations reduced
// to when they were active, how long they ran, which app produced them, whether
// risk analysis found anything, and a masked participant. It never reads
// chat_messages, so no path through it can return what was said.
type ChatMetadataService struct {
	projects   riskProjectResolver
	chats      chatMetadataReader
	references *subjectReferenceCodec
	budget     OperationBudget
	now        func() time.Time
}

// NewChatMetadataService composes the listing. A nil database, an invalid
// budget, or missing key material leaves the service nil, which registers the
// unavailable stub rather than a tool that always fails.
func NewChatMetadataService(db *pgxpool.Pool, budget OperationBudget, keyMaterial string) *ChatMetadataService {
	codec, err := newSubjectReferenceCodec(keyMaterial)
	if db == nil || err != nil || !budget.valid() {
		return nil
	}
	return &ChatMetadataService{
		projects:   postgresRiskProjectResolver{queries: platformrepo.New(db)},
		chats:      chatrepo.New(db),
		references: codec,
		budget:     budget,
		now:        time.Now,
	}
}

func (s *ChatMetadataService) valid() bool {
	return s != nil && s.projects != nil && s.chats != nil && s.references != nil && s.budget.valid() && s.now != nil
}

// ListChatsInput narrows one project's chats. Every filter is optional; an
// empty listing returns the chats active in the last week.
type ListChatsInput struct {
	ProjectID     string `json:"project_id,omitempty"`
	ProjectSlug   string `json:"project_slug,omitempty"`
	Window        string `json:"window,omitempty"`
	Risk          string `json:"risk,omitempty"`
	Source        string `json:"source,omitempty"`
	AssistantID   string `json:"assistant_id,omitempty"`
	UserReference string `json:"user_reference,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	Cursor        string `json:"cursor,omitempty"`
}

// ChatSummary is one chat reduced to what an investigation needs. It carries
// no title, no message content, no tool payloads, and no raw identity; the
// identity is masked and the reference is short-lived.
type ChatSummary struct {
	ChatID            string `json:"chat_id"`
	CreatedAt         string `json:"created_at"`
	LastMessageAt     string `json:"last_message_at"`
	MessageCount      int    `json:"message_count"`
	RiskFindingsCount int    `json:"risk_findings_count"`
	// Source is the observed chat source label, such as a coding agent's name.
	Source string `json:"source,omitempty"`
	// Client is the originating client the capture pipeline recorded.
	Client string `json:"client,omitempty"`
	// AccountType is the team or personal classification of the AI account
	// that produced the chat, when one is linked.
	AccountType string `json:"account_type,omitempty"`
	// AssistantID and AssistantName identify the administrator-configured
	// assistant behind the chat, when it was an assistant thread.
	AssistantID    string `json:"assistant_id,omitempty"`
	AssistantName  string `json:"assistant_name,omitempty"`
	MaskedIdentity string `json:"masked_identity,omitempty"`
	// UserReference narrows a follow-up list_chats call to the same person. It
	// is minted only for identities the listing can filter on.
	UserReference string `json:"user_reference,omitempty"`
}

type ListChatsOutput struct {
	Project      RiskProject   `json:"project"`
	Envelope     DataEnvelope  `json:"data"`
	Chats        []ChatSummary `json:"chats"`
	TotalMatches int           `json:"total_matches"`
	NextCursor   string        `json:"next_cursor,omitempty"`
	Limitations  string        `json:"limitations"`
}

// chatList is one validated listing: the normalized filters and the window
// they apply to. Everything a cursor must be bound to lives here.
type chatList struct {
	projectID   uuid.UUID
	window      ResolvedWindow
	now         time.Time
	risk        string
	source      string
	assistantID string
	limit       int
	// identity is the decoded user filter, "kind:identifier", or empty.
	identity string
}

func (s *ChatMetadataService) List(ctx context.Context, principal Principal, input ListChatsInput) (ListChatsOutput, error) {
	var zero ListChatsOutput
	if !s.valid() {
		return zero, ErrUnavailable
	}
	list, err := normalizeChatList(input)
	if err != nil {
		return zero, err
	}
	// Metered on the sensitive allowance: a page carries masked identities and
	// person references, so it must not be fundable by the summary budget.
	if err := s.budget.Allow(ctx, principal); err != nil {
		return zero, err
	}
	list.now = s.now()
	list.window, err = resolveWindow(input.Window, list.now, chatListWindowSpec)
	if err != nil {
		return zero, err
	}
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, fmt.Errorf("resolve chat list project: %w", err)
	}
	list.projectID = project.ID

	userID, externalUserID := "", ""
	if input.UserReference != "" {
		userID, externalUserID, err = s.chatListUserFilter(principal, input.UserReference, &list)
		if err != nil {
			return zero, err
		}
	}

	// The cursor resolves only against the query that minted it, so a position
	// cannot be replayed with different filters, another window, or a different
	// person. It also pins the absolute interval the first page read, so later
	// pages walk the same result set rather than one that slid with the clock.
	scope := list.cursorScope()
	offset, err := s.decodeChatListCursor(input.Cursor, principal, scope, &list)
	if err != nil {
		return zero, err
	}

	rows, err := s.chats.ListChats(ctx, chatrepo.ListChatsParams{
		ProjectID:      list.projectID,
		HasRiskFilter:  chatListHasRiskFilter(list.risk),
		MinRiskScore:   -1,
		ExternalUserID: externalUserID,
		UserID:         userID,
		Pinned:         "",
		// The search parameter also matches titles and emails, which would turn
		// the listing into a content oracle; it is never populated here.
		Search:            "",
		AssistantID:       list.assistantID,
		SourceKind:        "",
		ExcludeSourceKind: "",
		AccountType:       "",
		Sources:           nonEmpty(list.source),
		FromTime:          pgtype.Timestamptz{Time: list.window.start, InfinityModifier: pgtype.Finite, Valid: true},
		ToTime:            pgtype.Timestamptz{Time: list.window.end, InfinityModifier: pgtype.Finite, Valid: true},
		// The query lists chats by activity — last message at or after the
		// start, created at or before the end — so the order is activity too;
		// it has no creation-time order.
		SortBy:     "last_message_timestamp",
		SortOrder:  "desc",
		PageOffset: int32(offset),         // #nosec G115 -- the cursor codec caps offset at maxChatListTraversal.
		PageLimit:  int32(list.limit + 1), // #nosec G115 -- limit is clamped to maxChatListLimit; one extra row detects another page.
	})
	if err != nil {
		return zero, fmt.Errorf("list project chats: %w", err)
	}

	// Postgres is the system of record for chats, so the read is current
	// through the moment it ran; the envelope keeps the shape every other
	// project read carries.
	output := ListChatsOutput{
		Project:      riskProject(project),
		Envelope:     newDataEnvelope(list.now, list.now, list.window, len(rows) > 0),
		Chats:        []ChatSummary{},
		TotalMatches: 0,
		NextCursor:   "",
		Limitations:  chatListLimitations,
	}
	switch {
	case len(rows) > 0:
		output.TotalMatches = int(rows[0].TotalCount)
	case offset > 0:
		// Every page row carries the pre-LIMIT total; an empty page past the end
		// of a set that shrank has none, so the count is read on its own rather
		// than guessed from the cursor.
		total, err := s.chats.CountChats(ctx, chatrepo.CountChatsParams{
			FromTime:          pgtype.Timestamptz{Time: list.window.start, InfinityModifier: pgtype.Finite, Valid: true},
			ToTime:            pgtype.Timestamptz{Time: list.window.end, InfinityModifier: pgtype.Finite, Valid: true},
			HasRiskFilter:     chatListHasRiskFilter(list.risk),
			MinRiskScore:      -1,
			ProjectID:         list.projectID,
			ExternalUserID:    externalUserID,
			UserID:            userID,
			Pinned:            "",
			Search:            "",
			AssistantID:       list.assistantID,
			SourceKind:        "",
			ExcludeSourceKind: "",
			AccountType:       "",
			Sources:           nonEmpty(list.source),
		})
		if err != nil {
			return zero, fmt.Errorf("count project chats: %w", err)
		}
		output.TotalMatches = int(total)
	}
	rows, more := boundedRows(rows, list.limit)
	// Trimmed to what the traversal budget still allows, so the cap bounds the
	// chats actually handed over rather than only the number of pages.
	if remaining := maxChatListTraversal - offset; remaining < len(rows) {
		rows = rows[:max(remaining, 0)]
		more = false
	}
	chats := make([]ChatSummary, 0, len(rows))
	for _, row := range oneRowPerChat(rows, list.assistantID) {
		summary, err := s.chatSummary(principal, list, row)
		if err != nil {
			return zero, err
		}
		chats = append(chats, summary)
	}
	output.Chats = chats
	fitted, dropped, err := fitRows(output.Chats, func(chats []ChatSummary) any {
		candidate := output
		candidate.Chats = chats
		return candidate
	})
	if err != nil {
		return zero, err
	}
	output.Chats = fitted
	if dropped {
		// The page was cut to fit. The rows handed over are the ones whose chat
		// survived, counted in query rows so the cursor resumes at the right
		// offset even when a surviving chat spanned several thread rows.
		rows = rows[:rowsCoveringChats(rows, len(fitted))]
		more = len(rows) > 0
	}
	traversed := offset + len(rows)
	if more && len(rows) > 0 && traversed < maxChatListTraversal {
		cursor, err := s.references.EncodeScoped(principal, subjectKindCursor, scope, formatChatListCursor(traversed, list.window), list.now)
		if err != nil {
			return zero, fmt.Errorf("mint chat list cursor: %w", err)
		}
		output.NextCursor = cursor
	}
	return output, nil
}

// normalizeChatList validates the caller-supplied filters and turns them into
// the normalized listing a cursor is bound to. Anything outside the closed
// sets is refused rather than dropped: a filter silently ignored hands the
// caller a page it believes was narrowed. The limit alone is clamped rather
// than refused, because a too-large page is a preference, not a different
// question.
func normalizeChatList(input ListChatsInput) (chatList, error) {
	list := chatList{
		risk:   strings.TrimSpace(input.Risk),
		source: strings.TrimSpace(input.Source),
		limit:  input.Limit,
	}
	if input.ProjectID != "" && input.ProjectSlug != "" {
		return chatList{}, ErrChatListInvalid
	}
	switch list.risk {
	case "", ChatRiskWithFindings, ChatRiskWithoutFindings:
	default:
		return chatList{}, ErrChatListInvalid
	}
	if len(list.source) > maxChatSourceLength || strings.ContainsFunc(list.source, unicode.IsControl) {
		return chatList{}, ErrChatListInvalid
	}
	if input.AssistantID != "" {
		id, err := uuid.Parse(input.AssistantID)
		if err != nil {
			return chatList{}, ErrChatListInvalid
		}
		list.assistantID = id.String()
	}
	switch {
	case list.limit < 0:
		return chatList{}, ErrChatListInvalid
	case list.limit == 0:
		list.limit = defaultChatListLimit
	case list.limit > maxChatListLimit:
		list.limit = maxChatListLimit
	}
	return list, nil
}

// chatListHasRiskFilter spells the risk filter the way the chat query reads it.
func chatListHasRiskFilter(risk string) string {
	switch risk {
	case ChatRiskWithFindings:
		return "true"
	case ChatRiskWithoutFindings:
		return "false"
	default:
		return ""
	}
}

// cursorScope binds a cursor to the question that minted it: project, named
// window, and every filter. The absolute interval is not part of the scope —
// it travels inside the sealed cursor and is restored from it — because the
// scope has to be computable before the cursor is opened.
func (l chatList) cursorScope() string {
	return queryScope("list_chats", l.projectID.String(), string(l.window.Window), l.risk, l.source, l.assistantID, l.identity)
}

// chatListUserScope binds the person references a listing mints to the project
// alone, so a reference stays usable across windows and filters within the
// same investigation while never resolving in another project.
func chatListUserScope(projectID uuid.UUID) string {
	return queryScope("list_chats_user", projectID.String())
}

// chatListUserFilter resolves a person reference to the chat column and
// identifier it names. Only the two columns the chat query filters on are
// referenceable; an account email is masked on a row but never minted, because
// the query cannot narrow to it without matching titles as well.
func (s *ChatMetadataService) chatListUserFilter(principal Principal, reference string, list *chatList) (string, string, error) {
	subject, err := s.references.DecodeScoped(reference, principal, subjectKindUser, chatListUserScope(list.projectID), list.now)
	if err != nil {
		return "", "", ErrSubjectReferenceNotFound
	}
	identityKind, identifier, err := parseSubjectIdentity(subject)
	if err != nil {
		return "", "", ErrSubjectReferenceNotFound
	}
	list.identity = FormatSubjectIdentity(identityKind, identifier)
	switch identityKind {
	case SubjectIdentityUser:
		return identifier, "", nil
	case SubjectIdentityExternal:
		return "", identifier, nil
	default:
		return "", "", ErrSubjectReferenceNotFound
	}
}

// oneRowPerChat collapses the query's assistant fan-out. The chat query admits
// a chat by assistant and then left-joins every live assistant thread on it, so
// a chat several assistants worked in arrives as several rows, one per thread,
// adjacent because the order ends on the chat id. Each chat is reported once:
// as the thread of the assistant the listing was narrowed to when there is one,
// otherwise as its first thread row.
func oneRowPerChat(rows []chatrepo.ListChatsRow, assistantID string) []chatrepo.ListChatsRow {
	collapsed := make([]chatrepo.ListChatsRow, 0, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for _, row := range rows {
		position, seen := index[row.ID]
		if !seen {
			index[row.ID] = len(collapsed)
			collapsed = append(collapsed, row)
			continue
		}
		if assistantID != "" && uuidString(row.AssistantID) == assistantID {
			collapsed[position] = row
		}
	}
	return collapsed
}

// rowsCoveringChats counts the query rows that belong to the first n distinct
// chats, so an offset advanced past them lands on the next chat's first row.
func rowsCoveringChats(rows []chatrepo.ListChatsRow, n int) int {
	seen := make(map[uuid.UUID]struct{}, n)
	covered := 0
	for _, row := range rows {
		if _, ok := seen[row.ID]; !ok {
			if len(seen) == n {
				break
			}
			seen[row.ID] = struct{}{}
		}
		covered++
	}
	return covered
}

// chatSummary projects one row. The identity is masked and, when it is recorded
// under a column the listing can filter on, accompanied by a reference a
// follow-up listing can be narrowed with.
func (s *ChatMetadataService) chatSummary(principal Principal, list chatList, row chatrepo.ListChatsRow) (ChatSummary, error) {
	lastMessage := row.CreatedAt.Time
	if row.LastMessageTimestamp.Valid {
		lastMessage = row.LastMessageTimestamp.Time
	}
	summary := ChatSummary{
		ChatID:            row.ID.String(),
		CreatedAt:         row.CreatedAt.Time.UTC().Format(time.RFC3339),
		LastMessageAt:     lastMessage.UTC().Format(time.RFC3339),
		MessageCount:      int(row.NumMessages),
		RiskFindingsCount: int(row.RiskFindingsCount),
		Source:            findingLabel(conv.FromPGTextOrEmpty[string](row.Source)),
		Client:            findingLabel(row.OriginatingClient),
		AccountType:       findingLabel(row.AccountType),
		AssistantID:       uuidString(row.AssistantID),
		AssistantName:     findingLabel(conv.FromPGTextOrEmpty[string](row.AssistantName)),
	}
	identityKind, identifier := chatIdentity(row)
	if identifier == "" {
		return summary, nil
	}
	summary.MaskedIdentity = maskSubject(identifier)
	if identityKind == SubjectIdentityEmail {
		return summary, nil
	}
	reference, err := s.references.EncodeScoped(principal, subjectKindUser, chatListUserScope(list.projectID), FormatSubjectIdentity(identityKind, identifier), list.now)
	if err != nil {
		return ChatSummary{}, fmt.Errorf("mint chat list user reference: %w", err)
	}
	summary.UserReference = reference
	return summary, nil
}

// chatIdentity picks the person a chat is attributed to, preferring the
// platform user over an external user id over the linked AI account's email.
func chatIdentity(row chatrepo.ListChatsRow) (string, string) {
	if userID := conv.FromPGTextOrEmpty[string](row.UserID); userID != "" {
		return SubjectIdentityUser, userID
	}
	if externalUserID := conv.FromPGTextOrEmpty[string](row.ExternalUserID); externalUserID != "" {
		return SubjectIdentityExternal, externalUserID
	}
	if row.AccountEmail != "" {
		return SubjectIdentityEmail, row.AccountEmail
	}
	return "", ""
}

// A listing cursor carries the offset the next page resumes from and the
// absolute interval the first page read, minted through the same bound,
// expiring reference codec as everything else a caller holds between calls.
// Both travel inside the sealed token, so a caller can neither reset its own
// traversal budget nor slide the window by editing what it was handed.
func formatChatListCursor(offset int, window ResolvedWindow) string {
	return "o:" + strconv.Itoa(offset) + ":" + strconv.FormatInt(window.start.Unix(), 10) + ":" + strconv.FormatInt(window.end.Unix(), 10)
}

func parseChatListCursor(value string) (int, time.Time, time.Time, error) {
	rest, ok := strings.CutPrefix(value, "o:")
	if !ok {
		return 0, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 3 {
		return 0, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
	}
	offset, err := strconv.Atoi(parts[0])
	if err != nil || offset <= 0 || offset > maxChatListTraversal {
		return 0, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
	}
	start, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || start <= 0 {
		return 0, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
	}
	end, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || end <= start {
		return 0, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
	}
	return offset, time.Unix(start, 0).UTC(), time.Unix(end, 0).UTC(), nil
}

// decodeChatListCursor resolves a cursor against the listing's scope and pins
// the listing's window to the interval the cursor carries. Without a cursor
// the window resolved from the clock stands.
func (s *ChatMetadataService) decodeChatListCursor(cursor string, principal Principal, scope string, list *chatList) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	value, err := s.references.DecodeScoped(cursor, principal, subjectKindCursor, scope, list.now)
	if err != nil {
		return 0, ErrSubjectReferenceNotFound
	}
	offset, start, end, err := parseChatListCursor(value)
	if err != nil {
		return 0, err
	}
	list.window = ResolvedWindow{
		Window: list.window.Window,
		From:   start.Format(time.RFC3339),
		To:     end.Format(time.RFC3339),
		start:  start,
		end:    end,
	}
	return offset, nil
}
