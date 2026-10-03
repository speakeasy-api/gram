//nolint:exhaustruct // Diagnostic projections intentionally omit documented optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	defaultUserSearchLimit = 10
	maxUserSearchLimit     = 25
	// maxUserSearchTraversal bounds how many people one search may page through
	// in total. A page cap alone bounds a single response; without a traversal
	// cap a caller can still walk a project's whole directory one page at a
	// time, which is the enumeration this surface exists not to be.
	maxUserSearchTraversal = 100
	// minUserSearchQueryLength is the shortest partial identity a search
	// accepts. One or two characters match nearly everyone, which turns a
	// lookup for a known person into a listing of the project's people.
	minUserSearchQueryLength = 3
	maxUserSearchQueryLength = 200
	// maxUserMetricsTools bounds the per-tool breakdown on a person's summary.
	maxUserMetricsTools = 25
)

// User types a search may be asked about. They are the two identity columns
// telemetry attributes a person under: an organization's own people, keyed by
// email, and the end users of hosted MCP servers, keyed by external user id.
const (
	UserTypeInternal = "internal"
	UserTypeExternal = "external"
)

// ErrUserSearchInvalid is returned for an input this surface refuses.
var ErrUserSearchInvalid = errors.New("platform mcp user search input invalid")

// UserSearchReader is the per-person telemetry read model behind search_users
// and get_user_metrics_summary: the same two ClickHouse queries the
// dashboard's employee directory and employee page read from.
type UserSearchReader interface {
	SearchUsers(ctx context.Context, arg telemetryrepo.SearchUsersParams) ([]telemetryrepo.UserSummary, error)
	GetUserMetricsSummary(ctx context.Context, arg telemetryrepo.GetUserMetricsSummaryParams) (*telemetryrepo.MetricsSummaryRow, error)
}

// WithUserSearch attaches the project-wide people search and the per-person
// summary. They also require the drill-down composition (reference codec,
// sensitive budget, volume cap, auditor) because every page carries masked
// identities and person references exactly as list_mcp_usage_users does.
func (s *DiagnosticsService) WithUserSearch(reader UserSearchReader) *DiagnosticsService {
	s.userSearch = reader
	return s
}

// SearchUsersInput asks which people a partial identity matches in one
// project's telemetry. The query is required and must be specific enough to
// name someone rather than everyone.
type SearchUsersInput struct {
	ProjectID string `json:"project_id" jsonschema:"project ID to search"`
	Query     string `json:"query" jsonschema:"partial identity to match, case-insensitive, at least 3 characters: part of an email address, external user id, or user id"`
	UserType  string `json:"user_type,omitempty" jsonschema:"which people to search: internal (default) for the organization's own people, or external for end users of hosted MCP servers"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h, 7d (default), or 30d"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum people to return; defaults to 10 and is capped at 25"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"opaque cursor returned by a previous search_users result"`
}

// UserSearchMatch is one person, masked, with categorical evidence and a
// short-lived reference. Exact individual counts are deliberately absent here;
// get_user_metrics_summary answers those for one referenced person under its
// own audit entry.
type UserSearchMatch struct {
	UserReference  string `json:"user_reference"`
	MaskedIdentity string `json:"masked_identity"`
	Activity       string `json:"activity"`
	Errors         string `json:"errors"`
	LastSeenAt     string `json:"last_seen_at"`
}

type SearchUsersOutput struct {
	ProjectID  string            `json:"project_id"`
	UserType   string            `json:"user_type"`
	Envelope   DataEnvelope      `json:"data"`
	Users      []UserSearchMatch `json:"users"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

// userSearch is one validated search: the normalized query and the window it
// applies to. Everything a cursor must be bound to lives here.
type userSearch struct {
	projectID string
	query     string
	userType  string
	limit     int
	window    ResolvedWindow
	now       time.Time
}

func (s *DiagnosticsService) SearchUsers(ctx context.Context, principal Principal, input SearchUsersInput) (SearchUsersOutput, error) {
	search, err := normalizeUserSearch(input)
	if err != nil {
		return SearchUsersOutput{}, err
	}
	search.now = s.now()
	search.window, err = resolveWindow(input.Window, search.now, userSearchWindowSpec)
	if err != nil {
		return SearchUsersOutput{}, err
	}
	// project:read is the authorization boundary for the project-wide read,
	// exactly as it is for the project overview.
	if _, err := s.reader.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: search.projectID}); err != nil {
		return SearchUsersOutput{}, fmt.Errorf("authorize user search: %w", err)
	}
	// The cursor resolves only against the query that minted it, so a position
	// cannot be replayed with a different query, user type, or window. It also
	// restores the interval the first page read, replacing the one just
	// resolved from the clock.
	scope := search.cursorScope()
	position, err := s.decodeUserSearchCursor(input.Cursor, principal, scope, &search)
	if err != nil {
		return SearchUsersOutput{}, err
	}
	// Metered on the sensitive allowance: a page carries masked identities and
	// person references, so it must not be fundable by the summary budget. It
	// is charged only once the request is valid and authorized, so a malformed
	// or refused call cannot spend the caller's personal-data allowance.
	if err := s.sensitiveBudget.Allow(ctx, principal); err != nil {
		return SearchUsersOutput{}, err
	}
	// Charged for the page it may return, before the read rather than after: a
	// caller that cannot afford the rows should not spend the scan either.
	if err := s.volume.AllowRows(ctx, principal, search.limit); err != nil {
		return SearchUsersOutput{}, err
	}
	// Recorded before the read, and a failure to record refuses the call: a
	// search that returns people is an attribution read, and one that cannot
	// be audited leaves no trace of who looked up whom.
	if err := s.auditor.RecordUsageAttributionRead(ctx, principal, search.projectID, "project", search.projectID, "multiple", string(search.window.Window)); err != nil {
		return SearchUsersOutput{}, fmt.Errorf("record user search read: %w", err)
	}

	rows, err := s.userSearch.SearchUsers(ctx, telemetryrepo.SearchUsersParams{
		ExcludedHookSources: excludedHookSources(search.userType),
		GramProjectID:       search.projectID,
		TimeStart:           search.window.start.UnixNano(),
		TimeEnd:             search.window.end.UnixNano(),
		GramDeploymentID:    "",
		EventSource:         "",
		HookSource:          "",
		AccountType:         "",
		ExternalOrgID:       "",
		GroupBy:             userSearchGroupBy(search.userType),
		UserIDs:             nil,
		IdentityContains:    search.query,
		SortOrder:           "desc",
		Cursor:              position.key,
		// The boundary the previous page actually showed. Without it the
		// repository re-derives this person's last_seen from a lookup that
		// applies neither the window nor the Gram-hosted exclusion above, and a
		// person whose excluded rows run later than their qualifying ones is
		// handed back on every following page.
		CursorLastSeenUnixNano: position.lastSeen,
		// One extra row decides whether another page exists without a second
		// round trip, and is dropped before anything is projected.
		Limit:                search.limit + 1,
		MetricsDetail:        telemetryrepo.MetricsDetailFull,
		CanonicalIdentityOrg: s.userSearchCanonicalOrg(ctx, principal, search.userType),
	})
	if err != nil {
		return SearchUsersOutput{}, fmt.Errorf("search users: %w", err)
	}
	envelope, err := s.userSearchEnvelope(ctx, search.projectID, search.now, search.window, len(rows) > 0)
	if err != nil {
		return SearchUsersOutput{}, err
	}
	output := SearchUsersOutput{
		ProjectID: search.projectID,
		UserType:  search.userType,
		Envelope:  envelope,
		Users:     []UserSearchMatch{},
	}

	rows, more := boundedRows(rows, search.limit)
	// Trimmed to what the traversal budget still allows, so the cap bounds the
	// people actually handed over rather than only the number of pages.
	if remaining := maxUserSearchTraversal - position.traversed; remaining < len(rows) {
		rows = rows[:max(remaining, 0)]
		more = false
	}
	users := make([]UserSearchMatch, 0, len(rows))
	for _, row := range rows {
		identityKind, identifier := userSummaryIdentity(search.userType, row)
		reference, err := s.references.EncodeScoped(principal, subjectKindUser, projectUserScope(search.projectID), FormatSubjectIdentity(identityKind, identifier), search.now)
		if err != nil {
			return SearchUsersOutput{}, fmt.Errorf("mint user search reference: %w", err)
		}
		users = append(users, UserSearchMatch{
			UserReference:  reference,
			MaskedIdentity: maskSubject(identifier),
			Activity:       subjectActivity(row.ToolCallSuccess > 0, row.ToolCallFailure > 0),
			Errors:         subjectErrors(row.ToolCallFailure > 0),
			LastSeenAt:     time.Unix(0, row.LastSeenUnixNano).UTC().Format(time.RFC3339),
		})
	}
	output.Users = users
	fitted, dropped, err := fitRows(output.Users, func(users []UserSearchMatch) any {
		candidate := output
		candidate.Users = users
		return candidate
	})
	if err != nil {
		return SearchUsersOutput{}, err
	}
	output.Users = fitted
	rows, more = resumeAfterFit(rows, len(fitted), dropped, more)
	position.traversed += len(rows)
	if more && len(rows) > 0 && position.traversed < maxUserSearchTraversal {
		last := rows[len(rows)-1]
		position.key, position.lastSeen = last.UserID, last.LastSeenUnixNano
		// Sealed against search.window, which is the interval this page
		// actually read: the one the cursor restored on a later page, or the
		// one the clock resolved on the first. Every page of a traversal
		// therefore hands on the same bounds.
		cursor, err := s.references.EncodeScoped(principal, subjectKindCursor, scope, formatUserSearchCursor(position, search.window), search.now)
		if err != nil {
			return SearchUsersOutput{}, fmt.Errorf("mint user search cursor: %w", err)
		}
		output.NextCursor = cursor
	}
	return output, nil
}

// normalizeUserSearch validates the caller-supplied search and turns it into
// the normalized search a cursor is bound to. Anything outside the closed sets
// is refused rather than defaulted: a user type silently changed hands the
// caller a page about different people than it asked about.
func normalizeUserSearch(input SearchUsersInput) (userSearch, error) {
	search := userSearch{
		projectID: strings.TrimSpace(input.ProjectID),
		query:     strings.TrimSpace(input.Query),
		userType:  strings.ToLower(strings.TrimSpace(input.UserType)),
		limit:     input.Limit,
	}
	if search.projectID == "" {
		return userSearch{}, fmt.Errorf("%w: project_id is required", ErrUserSearchInvalid)
	}
	if length := utf8.RuneCountInString(search.query); length < minUserSearchQueryLength || length > maxUserSearchQueryLength {
		return userSearch{}, fmt.Errorf("%w: query must be between %d and %d characters", ErrUserSearchInvalid, minUserSearchQueryLength, maxUserSearchQueryLength)
	}
	switch search.userType {
	case "":
		search.userType = UserTypeInternal
	case UserTypeInternal, UserTypeExternal:
	default:
		return userSearch{}, fmt.Errorf("%w: user_type must be %s or %s", ErrUserSearchInvalid, UserTypeInternal, UserTypeExternal)
	}
	if search.limit <= 0 {
		search.limit = defaultUserSearchLimit
	}
	search.limit = min(search.limit, maxUserSearchLimit)
	return search, nil
}

// cursorScope is the normalized query a search cursor belongs to: the project,
// the people searched, the window, and the query text folded to the case the
// match ignores.
func (q userSearch) cursorScope() string {
	return queryScope("user_search", q.projectID, q.userType, string(q.window.Window), strings.ToLower(q.query))
}

// projectUserScope binds the person references a project-wide tool mints to
// the project alone, so a reference stays usable across windows and queries
// within the same investigation while never resolving in another project.
//
// The literal is the one search_tool_calls binds its references to, so a
// person found by either project-wide tool can be handed to the other.
func projectUserScope(projectID string) string {
	return queryScope("tool_call_search_user", projectID)
}

func userSearchGroupBy(userType string) string {
	if userType == UserTypeExternal {
		return "external_user_id"
	}
	return "user_id"
}

// excludedHookSources drops Gram-hosted inference from an organization's own
// people, whose sessions it is logged under but whose usage it is not. External
// users are the opposite case: their hosted-chat completions are their usage.
func excludedHookSources(userType string) []string {
	if userType == UserTypeExternal {
		return nil
	}
	return billing.GramHostedHookSourceNames()
}

// userSearchCanonicalOrg applies the identity fold to internal grouping only.
// External ids never fold: the identity map is email-keyed.
func (s *DiagnosticsService) userSearchCanonicalOrg(ctx context.Context, principal Principal, userType string) string {
	if userType == UserTypeExternal {
		return ""
	}
	return s.canonicalIdentityOrg(ctx, principal.OrganizationID)
}

// userSummaryIdentity names the column a search row's group key lives in, so
// the reference minted for it filters the right column later. Internal
// grouping keys email-first and falls back to a raw user id only for a person
// whose rows never carried an email; that fallback key is one of the raw user
// ids the summary folded, which is what identifies it, rather than the shape
// of the text, so a user id that happens to contain an @ is not mistaken for
// an email.
func userSummaryIdentity(userType string, row telemetryrepo.UserSummary) (string, string) {
	if userType == UserTypeExternal {
		return SubjectIdentityExternal, row.UserID
	}
	if slices.Contains(row.RawUserIDs, row.UserID) || !strings.Contains(row.UserID, "@") {
		return SubjectIdentityUser, row.UserID
	}
	return SubjectIdentityEmail, row.UserID
}

func (s *DiagnosticsService) userSearchEnvelope(ctx context.Context, projectID string, now time.Time, window ResolvedWindow, observed bool) (DataEnvelope, error) {
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{
		GramProjectIDs: []string{projectID},
	})
	if err != nil {
		return DataEnvelope{}, fmt.Errorf("read user search watermark: %w", err)
	}
	return newDataEnvelope(now, watermarkTime(watermark), window, observed), nil
}

// userSearchPosition is everything a later page restores from its cursor: the
// group key and last_seen to resume after, how far the traversal has reached,
// and the absolute interval the first page read.
type userSearchPosition struct {
	key       string
	lastSeen  int64
	traversed int
}

// A search cursor carries the whole position the repository resumes after — the
// group key, the last_seen the person was actually observed at on the page that
// minted it, how far the traversal has already reached, and the absolute
// interval the search began against — minted through the same bound, expiring
// reference codec as everything else a caller holds between calls. The key is
// frequently an email address, which is the reason the cursor is encrypted
// rather than merely signed.
//
// The timestamp is sealed in rather than looked up again because this search
// excludes Gram-hosted hook sources and applies a window, while the
// repository's cursor-lookup subquery applies neither: a person whose excluded
// rows run later than their qualifying ones would be re-derived at that later
// timestamp and returned again on every following page. search_tool_calls
// seals its own position for the same reason.
//
// The interval is sealed in because sealing the boundary alone only fixed the
// ordering. A window re-derived from the clock on each page slides forward, so
// the people nearest its original start — which, under newest-first ordering,
// are exactly the ones a later page is reaching — drop out of range before
// their page is requested, and the traversal ends early on a result the caller
// cannot tell from exhaustion. list_chats seals its interval for the same
// reason. Both the interval and the traversal count travel inside the sealed
// token, so a caller can neither slide its own window nor reset its budget by
// editing what it was handed. Nanoseconds, not list_chats' seconds, because the
// repository takes the interval in nanoseconds and this cursor's boundary is
// already one: no unit conversion sits between what is sealed and what is read
// back, so the bounds restored are bit-for-bit the ones the first page used.
//
// The "u3:" prefix is the version of this payload. Earlier shapes ("u:" before
// the boundary was sealed, "u2:" before the interval was) carry no interval to
// restore, and resuming one would silently recompute the window from the clock
// — the behaviour this version exists to remove — so they are refused as stale
// references instead. The tools are unreleased and references live 10 minutes,
// so nothing a caller can still be holding is worth a second parse path.
func formatUserSearchCursor(position userSearchPosition, window ResolvedWindow) string {
	return "u3:" + strconv.Itoa(position.traversed) +
		":" + strconv.FormatInt(window.start.UnixNano(), 10) +
		":" + strconv.FormatInt(window.end.UnixNano(), 10) +
		":" + strconv.FormatInt(position.lastSeen, 10) +
		":" + position.key
}

// parseUserSearchCursor recovers the traversal count, the sealed interval, the
// observed last_seen, and the group key. The key is taken as the whole
// remainder, so an identifier containing a colon survives the round trip.
func parseUserSearchCursor(value string) (userSearchPosition, time.Time, time.Time, error) {
	none := userSearchPosition{key: "", lastSeen: 0, traversed: 0}
	rest, ok := strings.CutPrefix(value, "u3:")
	if !ok {
		return none, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
	}
	fields := make([]string, 0, 4)
	for range 4 {
		field, remainder, found := strings.Cut(rest, ":")
		if !found {
			return none, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
		}
		fields = append(fields, field)
		rest = remainder
	}
	if rest == "" {
		return none, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
	}
	traversed, err := strconv.Atoi(fields[0])
	if err != nil || traversed < 0 || traversed > maxUserSearchTraversal {
		return none, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
	}
	start, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || start <= 0 {
		return none, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
	}
	end, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || end <= start {
		return none, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
	}
	// A zero last_seen would fall back to re-deriving the boundary, which is
	// the behaviour this cursor exists to avoid, so it is not a valid position.
	lastSeen, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || lastSeen <= 0 {
		return none, time.Time{}, time.Time{}, ErrSubjectReferenceNotFound
	}
	return userSearchPosition{key: rest, lastSeen: lastSeen, traversed: traversed},
		time.Unix(0, start).UTC(), time.Unix(0, end).UTC(), nil
}

// decodeUserSearchCursor resolves a cursor against the search's scope and pins
// the search's window to the interval the cursor carries, so every page of one
// traversal reads the same bounds. Without a cursor the window resolved from
// the clock stands. A cursor that cannot be opened, parsed, or that carries no
// interval is refused rather than resumed, so no page can fall back to
// recomputing the window from the clock.
func (s *DiagnosticsService) decodeUserSearchCursor(cursor string, principal Principal, scope string, search *userSearch) (userSearchPosition, error) {
	none := userSearchPosition{key: "", lastSeen: 0, traversed: 0}
	if cursor == "" {
		return none, nil
	}
	value, err := s.references.DecodeScoped(cursor, principal, subjectKindCursor, scope, search.now)
	if err != nil {
		return none, ErrSubjectReferenceNotFound
	}
	position, start, end, err := parseUserSearchCursor(value)
	if err != nil {
		return none, ErrSubjectReferenceNotFound
	}
	search.window = ResolvedWindow{
		Window: search.window.Window,
		From:   start.Format(time.RFC3339),
		To:     end.Format(time.RFC3339),
		start:  start,
		end:    end,
	}
	return position, nil
}

// GetUserMetricsSummaryInput names one person by a reference a summary tool
// minted, never by an email, account id, or name: the caller asks about
// someone it was shown, not about someone it can describe.
type GetUserMetricsSummaryInput struct {
	ProjectID     string `json:"project_id" jsonschema:"project ID to summarize"`
	UserReference string `json:"user_reference" jsonschema:"person reference from search_users, or from list_mcp_usage_users when the same mcp_id and window are supplied"`
	Window        string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h, 7d (default), or 30d"`
	MCPID         string `json:"mcp_id,omitempty" jsonschema:"configured MCP ID that minted the reference through list_mcp_usage_users; only needed for such a reference, and the summary itself stays project-wide"`
}

// UserMetricsTool is one tool the person called, with exact counts. Server is
// the MCP server a hook-recorded tool names, or the source a hosted tool was
// generated from; a native agent tool names no server and leaves it empty.
type UserMetricsTool struct {
	Tool     string `json:"tool"`
	Server   string `json:"server,omitempty"`
	Calls    int64  `json:"calls"`
	Failures int64  `json:"failures"`
}

// GetUserMetricsSummaryOutput is one person's activity over the window, in
// the shape the managed assistant's summary tool returned, without the raw
// identity. Tokens and cost are deliberately absent: they are a billing concern
// that does not belong in a diagnostic about a person.
type GetUserMetricsSummaryOutput struct {
	ProjectID string       `json:"project_id"`
	Envelope  DataEnvelope `json:"data"`

	// MaskedIdentity is enough to recognize a subject already known to the
	// administrator and not enough to learn one. It is never a raw identifier.
	MaskedIdentity string `json:"masked_identity"`
	Activity       string `json:"activity"`
	FirstSeenAt    string `json:"first_seen_at,omitempty"`
	LastSeenAt     string `json:"last_seen_at,omitempty"`

	ToolCalls       int64 `json:"tool_calls"`
	FailedToolCalls int64 `json:"failed_tool_calls"`
	// FailureRate is computed server-side and rounded to four decimal places.
	// Zero calls yields zero rather than an undefined ratio.
	FailureRate float64 `json:"failure_rate"`
	// ActiveServers counts the distinct servers the person's tools name, as
	// UserMetricsTool.Server derives them. Tools that name no server do not
	// count.
	ActiveServers  int64             `json:"active_servers"`
	TopTools       []UserMetricsTool `json:"top_tools"`
	ToolsTruncated bool              `json:"tools_truncated"`
}

func (s *DiagnosticsService) GetUserMetricsSummary(ctx context.Context, principal Principal, input GetUserMetricsSummaryInput) (GetUserMetricsSummaryOutput, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	mcpID := strings.TrimSpace(input.MCPID)
	if projectID == "" || strings.TrimSpace(input.UserReference) == "" {
		return GetUserMetricsSummaryOutput{}, fmt.Errorf("%w: project_id and user_reference are required", ErrUserSearchInvalid)
	}
	now := s.now()
	window, err := resolveWindow(input.Window, now, userSearchWindowSpec)
	if err != nil {
		return GetUserMetricsSummaryOutput{}, err
	}
	if _, err := s.reader.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: projectID}); err != nil {
		return GetUserMetricsSummaryOutput{}, fmt.Errorf("authorize user metrics summary: %w", err)
	}
	// Resolved only within the bound organization and connection generation.
	// An unknown, expired, cross-generation, or cross-project reference is a
	// single not-found: distinguishing them would confirm that a reference once
	// existed, which is itself information about another scope.
	identityKind, identifier, scopedToMCP, err := s.resolveProjectUserReference(ctx, principal, strings.TrimSpace(input.UserReference), projectID, mcpID, window, now)
	if err != nil {
		return GetUserMetricsSummaryOutput{}, err
	}
	maskedIdentity := maskSubject(identifier)
	// Metered on its own allowance: this read reaches personal data, so
	// exhausting it must not be possible by spending the ordinary diagnostic
	// budget. It is charged only once the request is valid, authorized, and
	// names a resolvable person, so a refused call cannot spend it.
	if err := s.sensitiveBudget.Allow(ctx, principal); err != nil {
		return GetUserMetricsSummaryOutput{}, err
	}
	// Recorded before the answer is composed, and a failure to record refuses
	// the call: an exact per-person summary that cannot be audited is one that
	// leaves no trace of who asked about whom. The target is the scope that
	// actually resolved the reference, so a project-wide reference stays
	// audited against the project even when an mcp_id was supplied beside it.
	targetKind, target := "project", projectID
	if scopedToMCP {
		targetKind, target = "mcp", mcpID
	}
	if err := s.auditor.RecordUsageAttributionRead(ctx, principal, projectID, targetKind, target, maskedIdentity, string(window.Window)); err != nil {
		return GetUserMetricsSummaryOutput{}, fmt.Errorf("record user metrics summary read: %w", err)
	}
	// Metric queries meter on their own count rather than on rows: one call
	// returns a handful of numbers but scans the whole window.
	if err := s.volume.AllowMetricQuery(ctx, principal); err != nil {
		return GetUserMetricsSummaryOutput{}, err
	}

	params := telemetryrepo.GetUserMetricsSummaryParams{
		ExcludedHookSources: nil,
		GramProjectID:       projectID,
		TimeStart:           window.start.UnixNano(),
		TimeEnd:             window.end.UnixNano(),
		User:                telemetryrepo.UserIdentity{UserIDs: nil, Emails: nil},
		CanonicalUser:       telemetryrepo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""},
		ExternalUserID:      "",
		EventSource:         "",
		HookSource:          "",
		AccountType:         "",
		ExternalOrgID:       "",
	}
	if identityKind == SubjectIdentityExternal {
		params.ExternalUserID = identifier
	} else {
		params.ExcludedHookSources = billing.GramHostedHookSourceNames()
		params.User, err = s.resolveUserIdentity(ctx, principal, projectID, window, identityKind, identifier)
		if err != nil {
			return GetUserMetricsSummaryOutput{}, err
		}
		// Both scopes are supplied, and the repository unions them. Neither
		// alone agrees with the search that offered this person: the fold
		// knows the employee's linked emails, while only the resolved set
		// knows the user ids an email the identity map does not carry is
		// recorded against — and for such an email the fold's id-keyed arm
		// resolves to no owner and matches nothing, so relying on it alone
		// reported no activity for a person search had just shown as active.
		// Dropping either one reopens that disagreement.
		params.CanonicalUser = canonicalUserIdentity(s.canonicalIdentityOrg(ctx, principal.OrganizationID), identityKind, identifier)
	}
	metrics, err := s.userSearch.GetUserMetricsSummary(ctx, params)
	if err != nil {
		return GetUserMetricsSummaryOutput{}, fmt.Errorf("read user metrics summary: %w", err)
	}
	observed := metrics != nil && metrics.LastSeenUnixNano > 0
	envelope, err := s.userSearchEnvelope(ctx, projectID, now, window, observed)
	if err != nil {
		return GetUserMetricsSummaryOutput{}, err
	}
	output := GetUserMetricsSummaryOutput{
		ProjectID:      projectID,
		Envelope:       envelope,
		MaskedIdentity: maskedIdentity,
		Activity:       SubjectStateInactive,
		TopTools:       []UserMetricsTool{},
	}
	if !observed {
		return output, nil
	}
	output.Activity = SubjectStateActive
	output.FirstSeenAt = time.Unix(0, metrics.FirstSeenUnixNano).UTC().Format(time.RFC3339)
	output.LastSeenAt = time.Unix(0, metrics.LastSeenUnixNano).UTC().Format(time.RFC3339)
	output.ToolCalls = boundedCount(metrics.TotalToolCalls)
	output.FailedToolCalls = boundedCount(metrics.ToolCallFailure)
	output.FailureRate = failureRate(output.ToolCalls, output.FailedToolCalls)
	output.TopTools, output.ActiveServers, output.ToolsTruncated = userMetricsTools(metrics)
	fitted, dropped, err := fitRows(output.TopTools, func(tools []UserMetricsTool) any {
		candidate := output
		candidate.TopTools = tools
		return candidate
	})
	if err != nil {
		return GetUserMetricsSummaryOutput{}, err
	}
	output.TopTools = fitted
	output.ToolsTruncated = output.ToolsTruncated || dropped
	return output, nil
}

// resolveProjectUserReference accepts a reference minted by a project-wide
// tool for this project, or by list_mcp_usage_users when the caller names the
// same MCP and window that minted it. It reports which of the two scopes
// resolved the reference, so the audit entry names the scope that authorized
// the read rather than whatever the caller happened to supply.
func (s *DiagnosticsService) resolveProjectUserReference(ctx context.Context, principal Principal, reference, projectID, mcpID string, window ResolvedWindow, now time.Time) (string, string, bool, error) {
	scopedToMCP := false
	subject, err := s.references.DecodeScoped(reference, principal, subjectKindUser, projectUserScope(projectID), now)
	if err != nil && mcpID != "" {
		// GetMCP is the authorization boundary for the MCP-scoped reference,
		// exactly as it is for the drill-down that minted it: a handle must not
		// outlive the caller's access to the server it was minted against.
		if _, err := s.reader.GetMCP(ctx, principal, GetMCPInput{ProjectID: projectID, MCPID: mcpID}); err != nil {
			return "", "", false, fmt.Errorf("resolve user reference mcp: %w", err)
		}
		subject, err = s.references.DecodeScoped(reference, principal, subjectKindUser, queryScope(projectID, mcpID, string(window.Window)), now)
		scopedToMCP = err == nil
	}
	if err != nil {
		return "", "", false, ErrSubjectReferenceNotFound
	}
	identityKind, identifier, err := parseSubjectIdentity(subject)
	if err != nil {
		return "", "", false, ErrSubjectReferenceNotFound
	}
	return identityKind, identifier, scopedToMCP, nil
}

// resolveUserIdentity widens one referenced identity to every identity the
// person's rows carry in the window, the same way the search grouped them.
// Hook events carry a user id and an email while token-bearing rows carry only
// the email, so a summary filtered on the reference alone would find one shape
// and silently drop the other.
func (s *DiagnosticsService) resolveUserIdentity(ctx context.Context, principal Principal, projectID string, window ResolvedWindow, identityKind, identifier string) (telemetryrepo.UserIdentity, error) {
	identity := telemetryrepo.UserIdentity{UserIDs: nil, Emails: nil}
	switch identityKind {
	case SubjectIdentityEmail:
		identity.Emails = []string{conv.NormalizeEmail(identifier)}
	case SubjectIdentityUser:
		identity.UserIDs = []string{identifier}
	default:
		return telemetryrepo.UserIdentity{}, ErrSubjectReferenceNotFound
	}
	rows, err := s.userSearch.SearchUsers(ctx, telemetryrepo.SearchUsersParams{
		ExcludedHookSources:    billing.GramHostedHookSourceNames(),
		GramProjectID:          projectID,
		TimeStart:              window.start.UnixNano(),
		TimeEnd:                window.end.UnixNano(),
		GramDeploymentID:       "",
		EventSource:            "",
		HookSource:             "",
		AccountType:            "",
		ExternalOrgID:          "",
		GroupBy:                "user_id",
		UserIDs:                []string{identifier},
		IdentityContains:       "",
		SortOrder:              "desc",
		Cursor:                 "",
		CursorLastSeenUnixNano: 0,
		Limit:                  1,
		MetricsDetail:          telemetryrepo.MetricsDetailBasic,
		CanonicalIdentityOrg:   s.canonicalIdentityOrg(ctx, principal.OrganizationID),
	})
	if err != nil {
		return telemetryrepo.UserIdentity{}, fmt.Errorf("resolve user identity: %w", err)
	}
	for _, row := range rows {
		identity.UserIDs = append(identity.UserIDs, row.RawUserIDs...)
		if strings.Contains(row.UserEmail, "@") {
			identity.Emails = append(identity.Emails, conv.NormalizeEmail(row.UserEmail))
		}
	}
	identity.UserIDs = conv.DedupeNonEmpty(identity.UserIDs)
	identity.Emails = conv.DedupeNonEmpty(identity.Emails)
	return identity, nil
}

// canonicalUserIdentity builds the identity-fold scope for a referenced person
// when the organization is on the fold. Its zero value disables canonical
// matching, and the repository then applies the literal identity set instead.
func canonicalUserIdentity(canonicalOrg, identityKind, identifier string) telemetryrepo.CanonicalUserIdentity {
	identity := telemetryrepo.CanonicalUserIdentity{OrgID: canonicalOrg, UserID: "", EmailLower: ""}
	if canonicalOrg == "" {
		return identity
	}
	if identityKind == SubjectIdentityEmail {
		identity.EmailLower = conv.NormalizeEmail(identifier)
	} else {
		identity.UserID = identifier
	}
	return identity
}

// userMetricsTools folds the per-tool maps to one row per tool, ordered by
// failures then calls so the broken tool leads, caps the list, and counts the
// distinct servers the tools name.
func userMetricsTools(metrics *telemetryrepo.MetricsSummaryRow) ([]UserMetricsTool, int64, bool) {
	tools := make([]UserMetricsTool, 0, len(metrics.ToolCounts))
	servers := map[string]struct{}{}
	for key, count := range metrics.ToolCounts {
		server := toolServer(key)
		if server != "" {
			servers[server] = struct{}{}
		}
		tools = append(tools, UserMetricsTool{
			Tool:     key,
			Server:   server,
			Calls:    boundedCount(count),
			Failures: boundedCount(metrics.ToolFailureCounts[key]),
		})
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Failures != tools[j].Failures {
			return tools[i].Failures > tools[j].Failures
		}
		if tools[i].Calls != tools[j].Calls {
			return tools[i].Calls > tools[j].Calls
		}
		return tools[i].Tool < tools[j].Tool
	})
	truncated := len(tools) > maxUserMetricsTools
	if truncated {
		tools = tools[:maxUserMetricsTools]
	}
	return tools, int64(len(servers)), truncated
}

// toolServer names the server a tool identity belongs to. A hook-recorded MCP
// tool follows the mcp__<server>__<tool> convention; a hosted tool is a tools:
// URN whose source segment is the document or function it was generated from.
// Anything else is a native agent tool with no server.
func toolServer(key string) string {
	if rest, ok := strings.CutPrefix(key, "mcp__"); ok {
		server, _, found := strings.Cut(rest, "__")
		if found {
			return server
		}
		return ""
	}
	tool, err := urn.ParseTool(key)
	if err != nil {
		return ""
	}
	return tool.Source
}
