//nolint:exhaustruct // Diagnostic projections intentionally omit documented optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	telemetrysvc "github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	defaultToolCallSearchLimit = 20
	maxToolCallSearchLimit     = 50
	// maxToolCallSearchTraversal bounds how many calls one search may page
	// through in total. A page cap alone bounds a single response; without a
	// traversal cap a caller can still walk an entire month one page at a time,
	// which is the export this surface exists not to be.
	maxToolCallSearchTraversal = 500
	// maxToolCallAttributeFilters bounds the attribute predicates one search
	// may combine. Each one is pushed down to the raw log scan.
	maxToolCallAttributeFilters = 5
	maxToolCallAttributeValues  = 10
	maxToolCallSearchTextLength = 200
	maxToolCallAttributeKeyLen  = 128
	maxToolCallAttributeValLen  = 256
	// maxAttributeKeysPerKind bounds each key list list_attribute_keys returns.
	maxAttributeKeysPerKind = 200
)

// Tool-call outcomes a search may narrow to. They are the trace-level outcomes
// the Tool Logs page filters on, spelled the way a result row reports them.
const (
	ToolCallOutcomeSuccess = "success"
	ToolCallOutcomeFailure = "failure"
	ToolCallOutcomeBlocked = "blocked"
	ToolCallOutcomePending = "pending"
	toolCallOutcomeUnknown = "unknown"
)

// Attribute paths the search maps its named text filters onto. Both resolve to
// columns the raw tool-call scan already projects, so no new ClickHouse SQL is
// needed to express them.
const (
	toolCallToolNameAttribute  = "gram.tool.name"
	toolCallHookErrorAttribute = "gram.hook.error"
	customAttributePrefix      = "@"
	customAttributeStoragePath = "app."
)

// toolCallAttributeOps is the closed set of attribute comparisons. "contains" is
// admitted for custom keys only: a substring predicate over a system attribute
// would let a caller probe content this surface never returns.
var toolCallAttributeOps = map[string]struct{}{
	"eq": {}, "not_eq": {}, "in": {}, "exists": {}, "not_exists": {}, "contains": {},
}

// toolCallIdentityAttributeSegments name path segments that carry a person's
// identifier on system attributes the platform does not materialize — a LiteLLM
// virtual key's user_email, say. The materialized identity columns are
// recognized exactly by telemetryrepo.IsIdentityAttributePath, which is guarded
// there against a new column appearing unclassified; the rest of the system
// namespace is open-ended, so a segment token is the only signal there is.
var toolCallIdentityAttributeSegments = []string{"email", "user_id", "userid", "username", "user_name", "upn"}

// toolCallContentAttributePrefixes name system attributes that carry tool
// inputs, outputs, or conversation content. They are refused as filters and
// withheld from key discovery, because a predicate over them is an oracle for
// the very content search_tool_calls promises never to return. HTTP headers are
// the same class and are handled by isHeaderAttribute, which needs a segment
// rule rather than a prefix list because header names are open-ended.
var toolCallContentAttributePrefixes = []string{
	"gen_ai.tool.call.",
	"gen_ai.prompt",
	"gen_ai.completion",
	"gen_ai.input",
	"gen_ai.output",
	"gen_ai.content",
	"gen_ai.system_instructions",
}

// toolCallHeaderAttributeSegment is the path segment an HTTP header hangs off.
// The platform stores request and response headers as map attributes —
// http.request.headers and http.response.headers, one entry per header name —
// and stamps a few individual headers under http.request.header.* and
// http.response.header.*. Header names are whatever the wire carried, so a
// segment is what a rule can be written against; a list of names could not stay
// complete.
const toolCallHeaderAttributeSegment = "header"

// validAttributeKey mirrors the path grammar the telemetry repository accepts.
// The repository silently drops a path that fails it; this surface refuses the
// request instead so a caller is never handed an unfiltered page it believes
// was narrowed.
var validAttributeKey = regexp.MustCompile(`^@?[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)*$`)

// ErrToolCallSearchInvalid is returned for an input this surface refuses.
var ErrToolCallSearchInvalid = errors.New("platform mcp tool call search input invalid")

// ToolCallSearchReader is the Tool Logs read model the project-wide search
// answers from: the same bounded trace summary list the dashboard's Tool Logs
// page and list_recent_tool_calls use, plus the attribute key inventory.
type ToolCallSearchReader interface {
	ListToolUsageTraces(ctx context.Context, arg telemetryrepo.ListToolUsageTracesParams) ([]telemetryrepo.ToolUsageTraceSummary, error)
	ListAttributeKeys(ctx context.Context, arg telemetryrepo.ListAttributeKeysParams) ([]string, error)
}

// WithToolCallSearch attaches the project-wide tool-call search and attribute
// key discovery. They also require the drill-down composition (reference
// codec, sensitive budget, volume cap, auditor) because a search page carries
// masked identities and person references exactly as list_mcp_usage_users does.
func (s *DiagnosticsService) WithToolCallSearch(search ToolCallSearchReader) *DiagnosticsService {
	if s != nil && search != nil {
		s.search = search
	}
	return s
}

// toolCallSearchValid reports whether search_tool_calls and list_attribute_keys
// are servable. Postgres is checked only where it is used, on the mcp_id
// attribution path, so the search stays honest about which dependency it needs.
func (s *DiagnosticsService) toolCallSearchValid() bool {
	return s != nil && s.search != nil && s.telemetry != nil && s.reader != nil && s.references != nil &&
		s.sensitiveBudget.valid() && s.volume.valid() && s.auditor != nil && s.budget.valid() && s.now != nil
}

// ToolCallAttributeFilter is one attribute predicate. Filters are combined with
// AND; a key is discovered with list_attribute_keys first.
type ToolCallAttributeFilter struct {
	Key    string   `json:"key" jsonschema:"attribute key as returned by list_attribute_keys: an @-prefixed custom key or a system key"`
	Op     string   `json:"op,omitempty" jsonschema:"comparison: eq (default), not_eq, in, exists, not_exists, or contains (custom @ keys only)"`
	Values []string `json:"values,omitempty" jsonschema:"values to compare against: one for eq, not_eq and contains, up to 10 for in, none for exists and not_exists"`
}

// SearchToolCallsInput narrows one project's tool calls. Every filter is
// optional; an empty search lists the newest calls in the window.
type SearchToolCallsInput struct {
	ProjectID        string                    `json:"project_id" jsonschema:"project ID to search"`
	Window           string                    `json:"window,omitempty" jsonschema:"observation window: 1h, 24h (default), 7d, or 30d. When a cursor is supplied the interval comes from the cursor and this must still name the window the cursor was minted for"`
	ToolNameContains string                    `json:"tool_name_contains,omitempty" jsonschema:"case-sensitive text the tool name must contain"`
	ErrorContains    string                    `json:"error_contains,omitempty" jsonschema:"case-sensitive text the recorded error message must contain; only calls that failed with an error message can match"`
	Outcome          string                    `json:"outcome,omitempty" jsonschema:"optional outcome filter: success, failure, blocked, or pending"`
	MCPID            string                    `json:"mcp_id,omitempty" jsonschema:"optional configured MCP ID, as returned by find_mcp or get_mcp, to narrow to one server; matches only calls the platform tied to that server, never calls an app merely reported under a matching name"`
	UserReference    string                    `json:"user_reference,omitempty" jsonschema:"optional person reference from a previous search_tool_calls row in this project, or from list_mcp_usage_users when the same mcp_id and window are supplied"`
	Attributes       []ToolCallAttributeFilter `json:"attributes,omitempty" jsonschema:"optional attribute filters, at most 5, combined with AND; discover keys with list_attribute_keys. A system key is refused when it identifies a person, in which case narrow to one person with user_reference instead, and when it carries tool content or an HTTP header. An @-prefixed custom key is the project's own integration data and allows every operator, whatever it is named"`
	Limit            int                       `json:"limit,omitempty" jsonschema:"maximum calls to return; defaults to 20 and is capped at 50"`
	Cursor           string                    `json:"cursor,omitempty" jsonschema:"opaque cursor returned by a previous search_tool_calls result; it pins the observation window to the interval the first page read, so paging a relative window does not drift as time passes. One search walks at most 500 calls in total, so a missing next cursor can mean that budget ran out rather than that nothing else matches"`
}

// ToolCallMatch is one tool call reduced to what an investigation needs. It
// carries no arguments, results, bodies, headers, URLs, trace IDs, or raw
// identities; the identity is masked and the reference is short-lived.
type ToolCallMatch struct {
	OccurredAt     string `json:"occurred_at"`
	ToolName       string `json:"tool_name,omitempty"`
	TargetType     string `json:"target_type"`
	TargetKind     string `json:"target_kind"`
	Target         string `json:"target,omitempty"`
	Outcome        string `json:"outcome"`
	Client         string `json:"client,omitempty"`
	MaskedIdentity string `json:"masked_identity,omitempty"`
	UserReference  string `json:"user_reference,omitempty"`
}

type SearchToolCallsOutput struct {
	ProjectID string `json:"project_id"`
	// MCPID echoes the server the search was narrowed to, when it was.
	MCPID    string       `json:"mcp_id,omitempty"`
	Envelope DataEnvelope `json:"data"`
	// Calls can be shorter than the requested limit, or empty, while more calls
	// matched: the response cap trims a page to what fits and the traversal
	// budget ends the walk outright. An empty page is therefore not the same
	// claim as the envelope's no_observations, which alone says the window
	// produced nothing at all.
	Calls []ToolCallMatch `json:"calls"`
	// NextCursor is absent both at the genuine end of the results and when the
	// traversal budget is spent, so its absence does not prove exhaustion.
	NextCursor string `json:"next_cursor,omitempty"`
	// AttributionUnavailable states that the configured MCP named by mcp_id
	// has no identity its telemetry is reliably recorded under, so nothing
	// could be attributed to it. It is reported beside the empty page rather
	// than left for a caller to mistake for an idle server. Calls a client
	// merely reported under this server's name are never counted as such an
	// identity, so a server known only that way reports unavailable instead of
	// being handed another server's history.
	AttributionUnavailable bool `json:"attribution_unavailable,omitempty"`
}

// toolCallSearch is one validated search: the normalized filters and the
// window they apply to. Everything a cursor must be bound to lives here.
type toolCallSearch struct {
	projectID  string
	mcpID      string
	window     ResolvedWindow
	now        time.Time
	toolName   string
	errorText  string
	outcome    string
	limit      int
	attributes []telemetryrepo.AttributeFilter
	// identity is the decoded user filter, "kind:identifier", or empty.
	identity string
}

func (s *DiagnosticsService) SearchToolCalls(ctx context.Context, principal Principal, input SearchToolCallsInput) (SearchToolCallsOutput, error) {
	if !s.toolCallSearchValid() {
		return SearchToolCallsOutput{}, ErrUnavailable
	}
	search, err := normalizeToolCallSearch(input)
	if err != nil {
		return SearchToolCallsOutput{}, err
	}
	// Metered on the sensitive allowance: a page carries masked identities and
	// person references, so it must not be fundable by the summary budget.
	if err := s.sensitiveBudget.Allow(ctx, principal); err != nil {
		return SearchToolCallsOutput{}, err
	}
	search.now = s.now()
	search.window, err = resolveWindow(input.Window, search.now, toolCallSearchWindowSpec)
	if err != nil {
		return SearchToolCallsOutput{}, err
	}
	// project:read is the authorization boundary for the project-wide read,
	// exactly as it is for list_recent_tool_calls and the project overview.
	if _, err := s.reader.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: search.projectID}); err != nil {
		return SearchToolCallsOutput{}, fmt.Errorf("authorize tool call search: %w", err)
	}

	output := SearchToolCallsOutput{
		ProjectID:              search.projectID,
		MCPID:                  search.mcpID,
		Envelope:               DataEnvelope{},
		Calls:                  []ToolCallMatch{},
		NextCursor:             "",
		AttributionUnavailable: false,
	}
	var server toolLogsTargets
	if search.mcpID != "" {
		server, err = s.toolCallSearchServer(ctx, principal, search)
		if err != nil {
			return SearchToolCallsOutput{}, err
		}
		if server.empty() {
			envelope, err := s.toolCallSearchEnvelope(ctx, search, false)
			if err != nil {
				return SearchToolCallsOutput{}, err
			}
			output.Envelope = envelope
			output.AttributionUnavailable = true
			return output, nil
		}
	}

	var userFilters []telemetryrepo.ToolUsageUserFilter
	if input.UserReference != "" {
		userFilters, err = s.toolCallSearchUserFilter(ctx, principal, input.UserReference, &search)
		if err != nil {
			return SearchToolCallsOutput{}, err
		}
	}

	// The cursor resolves only against the query that minted it, so a position
	// cannot be replayed with different filters, another window, or a different
	// person. Decoding it also pins the window to the interval the first page
	// read, which is why it happens before the read below.
	scope := search.cursorScope()
	position, err := s.decodeToolCallCursor(input.Cursor, principal, scope, &search)
	if err != nil {
		return SearchToolCallsOutput{}, err
	}
	traversed := position.traversed
	// Charged for the page it may return, before the read rather than after: a
	// caller that cannot afford the rows should not spend the scan either.
	if err := s.volume.AllowRows(ctx, principal, search.limit); err != nil {
		return SearchToolCallsOutput{}, err
	}

	matchers, err := s.toolCallSearchMatchers(ctx, search.projectID)
	if err != nil {
		return SearchToolCallsOutput{}, err
	}
	statuses := []string(nil)
	if search.outcome != "" {
		statuses = []string{search.outcome}
	}
	rows, err := s.search.ListToolUsageTraces(ctx, telemetryrepo.ListToolUsageTracesParams{
		GramProjectID:      search.projectID,
		TimeStart:          search.window.start.UnixNano(),
		TimeEnd:            search.window.end.UnixNano(),
		HostedMCPMatchers:  matchers.hosted,
		MCPServerMatchers:  matchers.servers,
		MetaMCPMatchers:    matchers.meta,
		TargetTypes:        nil,
		HostedToolsetSlugs: server.hostedToolsetSlugs,
		MCPServerTargetIDs: server.mcpServerTargetIDs,
		// Never set from a configured server: a shadow row is named by the
		// calling app, so selecting one by name would fold an unrelated
		// same-named server's calls into this server's history. See
		// serverIdentity.toolLogsTargets.
		ShadowServerNames: nil,
		MetaMCPServerIDs:  nil,
		UserFilters:       userFilters,
		// Folded exactly as the lists that produce a person reference are, so a
		// reference selects every address the person's calls are stored under
		// rather than only the one spelling the producer happened to report.
		CanonicalIdentityOrg: s.canonicalIdentityOrg(ctx, principal.OrganizationID),
		HookSources:          nil,
		ClientKeys:           nil,
		AccountType:          "",
		Statuses:             statuses,
		Query:                "",
		Filters:              search.repoFilters(),
		SortOrder:            "desc",
		CursorTimeUnixNano:   position.timeUnixNano,
		CursorID:             position.id,
		// One extra row decides whether another page exists without a second
		// round trip, and is dropped before anything is projected.
		Limit: search.limit + 1,
	})
	if err != nil {
		return SearchToolCallsOutput{}, fmt.Errorf("search tool calls: %w", err)
	}
	envelope, err := s.toolCallSearchEnvelope(ctx, search, len(rows) > 0)
	if err != nil {
		return SearchToolCallsOutput{}, err
	}
	output.Envelope = envelope

	rows, more := boundedRows(rows, search.limit)
	// Trimmed to what the traversal budget still allows, so the cap bounds the
	// calls actually handed over rather than only the number of pages.
	if remaining := maxToolCallSearchTraversal - traversed; remaining < len(rows) {
		rows = rows[:max(remaining, 0)]
		more = false
	}
	calls := make([]ToolCallMatch, 0, len(rows))
	for _, row := range rows {
		call, err := s.toolCallMatch(principal, search, row)
		if err != nil {
			return SearchToolCallsOutput{}, err
		}
		calls = append(calls, call)
	}
	output.Calls = calls
	fitted, dropped, err := fitRows(output.Calls, func(calls []ToolCallMatch) any {
		candidate := output
		candidate.Calls = calls
		return candidate
	})
	if err != nil {
		return SearchToolCallsOutput{}, err
	}
	output.Calls = fitted
	rows, more = resumeAfterFit(rows, len(fitted), dropped, more)
	traversed += len(rows)
	if more && len(rows) > 0 && traversed < maxToolCallSearchTraversal {
		last := rows[len(rows)-1]
		// search.window is the anchored interval, from the incoming cursor when
		// there was one, so the anchor is carried forward rather than re-minted
		// from the clock on every page.
		cursor, err := s.references.EncodeScoped(principal, subjectKindCursor, scope, formatToolCallCursor(last.StartTimeUnixNano, last.ID, traversed, search.window), search.now)
		if err != nil {
			return SearchToolCallsOutput{}, fmt.Errorf("mint tool call search cursor: %w", err)
		}
		output.NextCursor = cursor
	}
	return output, nil
}

// normalizeToolCallSearch validates the caller-supplied filters and turns them
// into the normalized search a cursor is bound to. Anything outside the closed
// sets is refused rather than dropped: a filter silently ignored hands the
// caller a page it believes was narrowed.
func normalizeToolCallSearch(input SearchToolCallsInput) (toolCallSearch, error) {
	search := toolCallSearch{
		projectID:  strings.TrimSpace(input.ProjectID),
		mcpID:      strings.TrimSpace(input.MCPID),
		window:     ResolvedWindow{},
		now:        time.Time{},
		toolName:   strings.TrimSpace(input.ToolNameContains),
		errorText:  strings.TrimSpace(input.ErrorContains),
		outcome:    strings.ToLower(strings.TrimSpace(input.Outcome)),
		limit:      input.Limit,
		attributes: nil,
		identity:   "",
	}
	if search.projectID == "" {
		return toolCallSearch{}, fmt.Errorf("%w: project_id is required", ErrToolCallSearchInvalid)
	}
	if len(search.toolName) > maxToolCallSearchTextLength || len(search.errorText) > maxToolCallSearchTextLength {
		return toolCallSearch{}, fmt.Errorf("%w: tool_name_contains and error_contains must be at most %d characters", ErrToolCallSearchInvalid, maxToolCallSearchTextLength)
	}
	if search.outcome != "" && !validToolCallOutcome(search.outcome) {
		return toolCallSearch{}, fmt.Errorf("%w: outcome must be one of %s, %s, %s, %s", ErrToolCallSearchInvalid, ToolCallOutcomeSuccess, ToolCallOutcomeFailure, ToolCallOutcomeBlocked, ToolCallOutcomePending)
	}
	if search.limit <= 0 {
		search.limit = defaultToolCallSearchLimit
	}
	search.limit = min(search.limit, maxToolCallSearchLimit)
	if len(input.Attributes) > maxToolCallAttributeFilters {
		return toolCallSearch{}, fmt.Errorf("%w: at most %d attribute filters are allowed", ErrToolCallSearchInvalid, maxToolCallAttributeFilters)
	}
	for _, filter := range input.Attributes {
		normalized, err := normalizeToolCallAttributeFilter(filter)
		if err != nil {
			return toolCallSearch{}, err
		}
		search.attributes = append(search.attributes, normalized)
	}
	return search, nil
}

func normalizeToolCallAttributeFilter(filter ToolCallAttributeFilter) (telemetryrepo.AttributeFilter, error) {
	key := strings.TrimSpace(filter.Key)
	if key == "" || len(key) > maxToolCallAttributeKeyLen || !validAttributeKey.MatchString(key) {
		return telemetryrepo.AttributeFilter{}, fmt.Errorf("%w: attribute key %q is not a valid attribute path", ErrToolCallSearchInvalid, key)
	}
	custom := strings.HasPrefix(key, customAttributePrefix)
	if !custom && isContentAttribute(key) {
		return telemetryrepo.AttributeFilter{}, fmt.Errorf("%w: attribute %q carries tool content and cannot be filtered on", ErrToolCallSearchInvalid, key)
	}
	// Refused for the same reason tool content is. Withholding a header value
	// from the result does not protect it while a caller can still test a guess
	// against it: eq or in over http.request.headers.Cookie answers whether the
	// guess was right by which calls come back. That is the oracle the content
	// refusal exists to close, so it closes here too.
	if !custom && isHeaderAttribute(key) {
		return telemetryrepo.AttributeFilter{}, fmt.Errorf("%w: attribute %q carries an HTTP header and cannot be filtered on: a predicate over a header value tests a guess against content this surface never returns", ErrToolCallSearchInvalid, key)
	}
	// Refused, not routed. Narrowing to one named person is an attribution read
	// that RecordUsageAttributionRead has to record first, and user_reference is
	// where that happens: it resolves a bound, expiring handle, records the read,
	// and refuses the whole call when the record cannot be written. An identity
	// attribute would reach the same rows as an ordinary predicate, past the
	// audit and past the reference's own scoping. Routing it to the audited path
	// instead would need the set of identity-bearing keys to be complete forever;
	// refusing leaves one door, and a key this surface fails to recognize
	// narrows nothing rather than reading unaudited.
	if !custom && isIdentityAttribute(key) {
		return telemetryrepo.AttributeFilter{}, fmt.Errorf("%w: attribute %q identifies a person; narrow to one person with user_reference, which is recorded as an attribution read", ErrToolCallSearchInvalid, key)
	}
	op := strings.ToLower(strings.TrimSpace(filter.Op))
	if op == "" {
		op = "eq"
	}
	if _, ok := toolCallAttributeOps[op]; !ok {
		return telemetryrepo.AttributeFilter{}, fmt.Errorf("%w: attribute op must be one of eq, not_eq, in, exists, not_exists, contains", ErrToolCallSearchInvalid)
	}
	if op == "contains" && !custom {
		return telemetryrepo.AttributeFilter{}, fmt.Errorf("%w: contains is allowed on custom @ attributes only", ErrToolCallSearchInvalid)
	}
	values := make([]string, 0, len(filter.Values))
	for _, value := range filter.Values {
		if len(value) > maxToolCallAttributeValLen {
			return telemetryrepo.AttributeFilter{}, fmt.Errorf("%w: attribute values must be at most %d characters", ErrToolCallSearchInvalid, maxToolCallAttributeValLen)
		}
		values = append(values, value)
	}
	switch op {
	case "exists", "not_exists":
		if len(values) != 0 {
			return telemetryrepo.AttributeFilter{}, fmt.Errorf("%w: %s takes no values", ErrToolCallSearchInvalid, op)
		}
	case "in":
		if len(values) == 0 || len(values) > maxToolCallAttributeValues {
			return telemetryrepo.AttributeFilter{}, fmt.Errorf("%w: in takes between 1 and %d values", ErrToolCallSearchInvalid, maxToolCallAttributeValues)
		}
	default:
		if len(values) != 1 {
			return telemetryrepo.AttributeFilter{}, fmt.Errorf("%w: %s takes exactly one value", ErrToolCallSearchInvalid, op)
		}
	}
	return telemetryrepo.AttributeFilter{Path: key, Op: op, Values: values}, nil
}

// isContentAttribute reports whether a system attribute carries tool inputs,
// outputs, or conversation content.
func isContentAttribute(key string) bool {
	for _, prefix := range toolCallContentAttributePrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// isHeaderAttribute reports whether a system attribute path descends through an
// HTTP header. Any segment beginning with "header" counts, which covers both the
// http.request.headers / http.response.headers maps, whose own keys are header
// names, and the individual http.request.header.* and http.response.header.*
// attributes the server stamps.
//
// The boundary is deliberate and narrow. It refuses two platform attributes an
// investigation might otherwise have reached: http.request.header.user_agent and
// http.response.header.www_authenticate. The calling app is still reported on
// every result row and remains filterable through gram.mcp.client.name, and an
// upstream's rejection is still reachable through error_contains, the outcome
// filter, and get_mcp_diagnostics. The http.* attributes that are not headers —
// http.request.method, http.route, http.response.status_code,
// http.server.request.duration, http.request.body — carry no header segment and
// are untouched.
//
// Custom "@" keys are out of scope here exactly as they are for identity and
// content: they hold whatever a project's own integrations attached, and every
// operator is documented as allowed on them.
func isHeaderAttribute(key string) bool {
	for segment := range strings.SplitSeq(key, ".") {
		if strings.HasPrefix(strings.ToLower(segment), toolCallHeaderAttributeSegment) {
			return true
		}
	}
	return false
}

// isIdentityAttribute reports whether a system attribute selects one named
// person. The platform's own identity columns are recognized exactly; any other
// system path is treated as identity-bearing when one of its segments carries an
// identifier token, which is what catches an identity attribute the platform
// records without materializing.
//
// Custom "@" keys are deliberately out of scope: they hold whatever a project's
// integrations attached, carry no platform identity semantics, and every
// operator is documented as allowed on them. A project that attaches its own
// person identifier as a custom attribute can therefore still be filtered on it,
// and the audited user_reference remains the only way to select by the identity
// the platform itself recorded.
func isIdentityAttribute(key string) bool {
	if telemetryrepo.IsIdentityAttributePath(key) {
		return true
	}
	for segment := range strings.SplitSeq(key, ".") {
		for _, token := range toolCallIdentityAttributeSegments {
			if strings.Contains(segment, token) {
				return true
			}
		}
	}
	return false
}

func validToolCallOutcome(outcome string) bool {
	switch outcome {
	case ToolCallOutcomeSuccess, ToolCallOutcomeFailure, ToolCallOutcomeBlocked, ToolCallOutcomePending:
		return true
	default:
		return false
	}
}

// repoFilters folds the named text filters and the attribute filters into the
// predicate list the repository applies. The tool name and hook error are
// attributes the raw scan already projects as columns, so expressing them this
// way reuses the Tool Logs query rather than adding SQL.
func (q toolCallSearch) repoFilters() []telemetryrepo.AttributeFilter {
	filters := make([]telemetryrepo.AttributeFilter, 0, len(q.attributes)+2)
	if q.toolName != "" {
		filters = append(filters, telemetryrepo.AttributeFilter{Path: toolCallToolNameAttribute, Op: "contains", Values: []string{q.toolName}})
	}
	if q.errorText != "" {
		filters = append(filters, telemetryrepo.AttributeFilter{Path: toolCallHookErrorAttribute, Op: "contains", Values: []string{q.errorText}})
	}
	return append(filters, q.attributes...)
}

// cursorScope is the normalized query a search cursor belongs to: every filter,
// the window, and the decoded person. The attribute filters are rendered in
// order so the same predicates in a different order are a different scope,
// which is the conservative reading.
func (q toolCallSearch) cursorScope() string {
	parts := []string{"tool_call_search", q.projectID, q.mcpID, string(q.window.Window), q.toolName, q.errorText, q.outcome, q.identity}
	for _, filter := range q.attributes {
		parts = append(parts, filter.Path, filter.Op, strings.Join(filter.Values, "\x1f"))
	}
	return queryScope(parts...)
}

// toolCallSearchUserScope binds the person references a search page mints to
// the project alone, so a reference stays usable across windows and filters
// within the same investigation while never resolving in another project.
func toolCallSearchUserScope(projectID string) string {
	return queryScope("tool_call_search_user", projectID)
}

// toolCallSearchServer resolves mcp_id to the targets a trace-level read may
// narrow to. It delegates to serverIdentity, the one resolution of a
// configured MCP in this package, and takes from it only what
// toolLogsTargets admits: the identities the platform itself stamped, never
// the names an agent reported. GetMCP is the authorization boundary, failing
// closed for an MCP this principal cannot see.
//
// Empty targets mean nothing reliable identifies the server, which the caller
// reports rather than widening to the whole project.
func (s *DiagnosticsService) toolCallSearchServer(ctx context.Context, principal Principal, search toolCallSearch) (toolLogsTargets, error) {
	if s.db == nil {
		return toolLogsTargets{}, ErrUnavailable
	}
	if _, err := s.reader.GetMCP(ctx, principal, GetMCPInput{ProjectID: search.projectID, MCPID: search.mcpID}); err != nil {
		return toolLogsTargets{}, fmt.Errorf("resolve tool call search mcp: %w", err)
	}
	identity, err := s.serverIdentity(ctx, principal.OrganizationID, search.projectID, search.mcpID)
	if err != nil {
		return toolLogsTargets{}, err
	}
	return identity.toolLogsTargets(), nil
}

// toolCallSearchUserFilter resolves a person reference to the telemetry column
// and identifier it names, records the attribution read, and returns the
// filter. A reference is accepted from a previous search page in this project,
// or from list_mcp_usage_users when the search is narrowed to the same MCP and
// window that minted it.
func (s *DiagnosticsService) toolCallSearchUserFilter(ctx context.Context, principal Principal, reference string, search *toolCallSearch) ([]telemetryrepo.ToolUsageUserFilter, error) {
	subject, err := s.references.DecodeScoped(reference, principal, subjectKindUser, toolCallSearchUserScope(search.projectID), search.now)
	if err != nil && search.mcpID != "" {
		subject, err = s.references.DecodeScoped(reference, principal, subjectKindUser, queryScope(search.projectID, search.mcpID, string(search.window.Window)), search.now)
	}
	if err != nil {
		return nil, ErrSubjectReferenceNotFound
	}
	identityKind, identifier, err := parseSubjectIdentity(subject)
	if err != nil {
		return nil, ErrSubjectReferenceNotFound
	}
	kind, ok := toolUsageUserKind(identityKind)
	if !ok {
		return nil, ErrSubjectReferenceNotFound
	}
	search.identity = FormatSubjectIdentity(identityKind, identifier)
	// Recorded before the read, and a failure to record refuses the call: a
	// search narrowed to one person is an attribution read, and one that cannot
	// be audited leaves no trace of who asked about whom.
	targetKind, target := "project", search.projectID
	if search.mcpID != "" {
		targetKind, target = "mcp", search.mcpID
	}
	if err := s.auditor.RecordUsageAttributionRead(ctx, principal, search.projectID, targetKind, target, maskSubject(identifier), string(search.window.Window)); err != nil {
		return nil, fmt.Errorf("record tool call search attribution read: %w", err)
	}
	return []telemetryrepo.ToolUsageUserFilter{{Kind: kind, Key: identifier}}, nil
}

// Identity kinds cross two vocabularies: a subject reference names the column a
// person is recorded under, and the Tool Logs query names the same columns as
// user kinds. Agents and unknown actors have no reference kind and so are never
// filterable by reference.
func toolUsageUserKind(identityKind string) (string, bool) {
	switch identityKind {
	case SubjectIdentityEmail:
		return "email", true
	case SubjectIdentityExternal:
		return "external_user_id", true
	case SubjectIdentityUser:
		return "user_id", true
	default:
		return "", false
	}
}

func subjectIdentityKind(userKind string) (string, bool) {
	switch userKind {
	case "email":
		return SubjectIdentityEmail, true
	case "external_user_id":
		return SubjectIdentityExternal, true
	case "user_id":
		return SubjectIdentityUser, true
	default:
		return "", false
	}
}

// toolUsageMatchers are the configured-server identities the Tool Logs query
// folds observed calls onto, loaded from Postgres.
type toolUsageMatchers struct {
	hosted  []telemetryrepo.HostedMCPMatcher
	servers []telemetryrepo.MCPServerMatcher
	meta    []telemetryrepo.MetaMCPMatcher
}

func (s *DiagnosticsService) toolCallSearchMatchers(ctx context.Context, projectID string) (toolUsageMatchers, error) {
	// Without Postgres the rows still list, but hook-observed calls cannot be
	// folded onto the configured servers and are reported as the calling app
	// named them.
	if s.db == nil {
		return toolUsageMatchers{hosted: nil, servers: nil, meta: nil}, nil
	}
	parsedProject, err := uuid.Parse(projectID)
	if err != nil {
		return toolUsageMatchers{}, fmt.Errorf("parse project id: %w", err)
	}
	hosted, servers, err := telemetrysvc.LoadToolUsageMatchers(ctx, s.db, parsedProject)
	if err != nil {
		return toolUsageMatchers{}, fmt.Errorf("load tool call search matchers: %w", err)
	}
	meta, err := telemetrysvc.LoadMetaMCPMatchers(ctx, s.db, parsedProject)
	if err != nil {
		return toolUsageMatchers{}, fmt.Errorf("load tool call search gateway matchers: %w", err)
	}
	return toolUsageMatchers{hosted: hosted, servers: servers, meta: meta}, nil
}

func (s *DiagnosticsService) toolCallSearchEnvelope(ctx context.Context, search toolCallSearch, observed bool) (DataEnvelope, error) {
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{
		GramProjectIDs: []string{search.projectID},
	})
	if err != nil {
		return DataEnvelope{}, fmt.Errorf("read tool call search watermark: %w", err)
	}
	return newDataEnvelope(search.now, watermarkTime(watermark), search.window, observed), nil
}

// toolCallMatch projects one trace summary. The identity is masked and, when
// it is a person recorded under a referenceable column, accompanied by a
// reference this search can be narrowed with. Agents and unknown actors get a
// masked label and no reference.
func (s *DiagnosticsService) toolCallMatch(principal Principal, search toolCallSearch, row telemetryrepo.ToolUsageTraceSummary) (ToolCallMatch, error) {
	match := ToolCallMatch{
		OccurredAt:     time.Unix(0, row.StartTimeUnixNano).UTC().Format(time.RFC3339Nano),
		ToolName:       row.ToolName,
		TargetType:     row.TargetType,
		TargetKind:     row.TargetKind,
		Target:         recentToolCallTarget(row),
		Outcome:        toolCallOutcome(row),
		Client:         dereferenceString(row.HookSource),
		MaskedIdentity: "",
		UserReference:  "",
	}
	if row.UserKind == "" || row.UserKind == "unknown" || row.UserKey == "" {
		return match, nil
	}
	match.MaskedIdentity = maskSubject(row.UserKey)
	identityKind, ok := subjectIdentityKind(row.UserKind)
	if !ok {
		return match, nil
	}
	reference, err := s.references.EncodeScoped(principal, subjectKindUser, toolCallSearchUserScope(search.projectID), FormatSubjectIdentity(identityKind, row.UserKey), search.now)
	if err != nil {
		return ToolCallMatch{}, fmt.Errorf("mint tool call search user reference: %w", err)
	}
	match.UserReference = reference
	return match, nil
}

// toolCallOutcome reports a trace's outcome in the vocabulary the outcome
// filter accepts, so a caller can narrow to exactly what a row said.
func toolCallOutcome(row telemetryrepo.ToolUsageTraceSummary) string {
	if row.HookStatus != nil {
		return *row.HookStatus
	}
	if row.HTTPStatusCode == nil {
		return toolCallOutcomeUnknown
	}
	switch {
	case *row.HTTPStatusCode >= 400:
		return ToolCallOutcomeFailure
	case *row.HTTPStatusCode >= 200:
		return ToolCallOutcomeSuccess
	default:
		return toolCallOutcomeUnknown
	}
}

// toolCallCursorPrefix tags the cursor payload's shape. It was bumped when the
// window bounds were added: a cursor minted before then carries no interval to
// resume against, and resuming it would silently recompute the window from the
// clock — exactly the drift the bounds exist to prevent. These tools are
// unreleased, so such a cursor is refused rather than supported as a second
// shape.
const toolCallCursorPrefix = "s2:"

// toolCallCursor is the position a later page resumes from: the page key the
// repository orders by (event time and summary id), how far the traversal has
// already reached, and the absolute interval the first page read.
type toolCallCursor struct {
	timeUnixNano int64
	id           string
	traversed    int
	windowStart  time.Time
	windowEnd    time.Time
}

// A search cursor is minted through the same bound, expiring reference codec as
// everything else a caller holds between calls. Both the traversal count and
// the window bounds travel inside the sealed token, so a caller can neither
// reset its own traversal budget nor slide the window by editing what it was
// handed. The bounds are what keep paging a relative window stable: without
// them, "the last 24h" is recomputed from the clock on every page, and a call
// near the original start silently falls out of range between requests.
func formatToolCallCursor(unixNano int64, id string, traversed int, window ResolvedWindow) string {
	return toolCallCursorPrefix + strconv.FormatInt(unixNano, 10) +
		":" + strconv.Itoa(traversed) +
		":" + strconv.FormatInt(window.start.Unix(), 10) +
		":" + strconv.FormatInt(window.end.Unix(), 10) +
		":" + id
}

func parseToolCallCursor(value string) (toolCallCursor, error) {
	rest, ok := strings.CutPrefix(value, toolCallCursorPrefix)
	if !ok {
		return toolCallCursor{}, ErrSubjectReferenceNotFound
	}
	timestamp, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return toolCallCursor{}, ErrSubjectReferenceNotFound
	}
	count, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return toolCallCursor{}, ErrSubjectReferenceNotFound
	}
	from, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return toolCallCursor{}, ErrSubjectReferenceNotFound
	}
	to, id, ok := strings.Cut(rest, ":")
	if !ok || id == "" {
		return toolCallCursor{}, ErrSubjectReferenceNotFound
	}
	position, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || position <= 0 {
		return toolCallCursor{}, ErrSubjectReferenceNotFound
	}
	traversed, err := strconv.Atoi(count)
	if err != nil || traversed < 0 || traversed > maxToolCallSearchTraversal {
		return toolCallCursor{}, ErrSubjectReferenceNotFound
	}
	start, err := strconv.ParseInt(from, 10, 64)
	if err != nil || start <= 0 {
		return toolCallCursor{}, ErrSubjectReferenceNotFound
	}
	end, err := strconv.ParseInt(to, 10, 64)
	if err != nil || end <= start {
		return toolCallCursor{}, ErrSubjectReferenceNotFound
	}
	return toolCallCursor{
		timeUnixNano: position,
		id:           id,
		traversed:    traversed,
		windowStart:  time.Unix(start, 0).UTC(),
		windowEnd:    time.Unix(end, 0).UTC(),
	}, nil
}

// decodeToolCallCursor resolves a cursor against the query that minted it and
// pins the search to the interval that query read, so every later page reads
// the same window rather than one recomputed from the clock. Without a cursor
// the window resolved from the clock stands. A cursor that carries no interval
// is refused by parseToolCallCursor, so there is no path back to recomputing it.
func (s *DiagnosticsService) decodeToolCallCursor(cursor string, principal Principal, scope string, search *toolCallSearch) (toolCallCursor, error) {
	if cursor == "" {
		return toolCallCursor{}, nil
	}
	value, err := s.references.DecodeScoped(cursor, principal, subjectKindCursor, scope, search.now)
	if err != nil {
		return toolCallCursor{}, ErrSubjectReferenceNotFound
	}
	position, err := parseToolCallCursor(value)
	if err != nil {
		return toolCallCursor{}, ErrSubjectReferenceNotFound
	}
	search.window = ResolvedWindow{
		Window: search.window.Window,
		From:   position.windowStart.Format(time.RFC3339),
		To:     position.windowEnd.Format(time.RFC3339),
		start:  position.windowStart,
		end:    position.windowEnd,
	}
	return position, nil
}

// ListAttributeKeysInput asks which attribute keys one project's telemetry
// carried in a window, so a caller can choose filters before searching.
type ListAttributeKeysInput struct {
	ProjectID string `json:"project_id" jsonschema:"project ID to inspect"`
	Window    string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h, 7d (default), or 30d"`
}

type ListAttributeKeysOutput struct {
	ProjectID string       `json:"project_id"`
	Envelope  DataEnvelope `json:"data"`
	// CustomKeys are the @-prefixed attributes the project's own integrations
	// attached; every operator is allowed on them. Capped at
	// maxAttributeKeysPerKind, so this is not necessarily every custom key the
	// window carried — see Truncated.
	CustomKeys []string `json:"custom_keys"`
	// SystemKeys are the platform-recorded attributes that may be filtered on.
	// Keys that carry tool content, keys that carry an HTTP header, and keys
	// that identify a person are withheld here because a search refuses them:
	// one person's calls are narrowed to with user_reference, which records the
	// attribution read. Capped the same way as CustomKeys.
	SystemKeys []string `json:"system_keys"`
	// Truncated states that at least one kind held more than
	// maxAttributeKeysPerKind keys and was cut. It is the only signal that a
	// key's absence from the lists above does not mean the project never
	// recorded it, so the tool description points a caller at it.
	Truncated bool `json:"truncated"`
}

func (s *DiagnosticsService) ListAttributeKeys(ctx context.Context, principal Principal, input ListAttributeKeysInput) (ListAttributeKeysOutput, error) {
	if !s.toolCallSearchValid() {
		return ListAttributeKeysOutput{}, ErrUnavailable
	}
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return ListAttributeKeysOutput{}, fmt.Errorf("%w: project_id is required", ErrToolCallSearchInvalid)
	}
	// Metered on the summary allowance: a key inventory names no subject and
	// reads a pre-aggregated view.
	if err := s.budget.Allow(ctx, principal); err != nil {
		return ListAttributeKeysOutput{}, err
	}
	now := s.now()
	window, err := resolveWindow(input.Window, now, attributeKeysWindowSpec)
	if err != nil {
		return ListAttributeKeysOutput{}, err
	}
	if _, err := s.reader.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: projectID}); err != nil {
		return ListAttributeKeysOutput{}, fmt.Errorf("authorize attribute key listing: %w", err)
	}
	keys, err := s.search.ListAttributeKeys(ctx, telemetryrepo.ListAttributeKeysParams{
		GramProjectID: projectID,
		TimeStart:     window.start.UnixNano(),
		TimeEnd:       window.end.UnixNano(),
	})
	if err != nil {
		return ListAttributeKeysOutput{}, fmt.Errorf("list attribute keys: %w", err)
	}
	custom, system := splitAttributeKeys(keys)
	watermark, err := s.telemetry.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{GramProjectIDs: []string{projectID}})
	if err != nil {
		return ListAttributeKeysOutput{}, fmt.Errorf("read attribute keys watermark: %w", err)
	}
	truncated := len(custom) > maxAttributeKeysPerKind || len(system) > maxAttributeKeysPerKind
	custom = custom[:min(len(custom), maxAttributeKeysPerKind)]
	system = system[:min(len(system), maxAttributeKeysPerKind)]
	return ListAttributeKeysOutput{
		ProjectID:  projectID,
		Envelope:   newDataEnvelope(now, watermarkTime(watermark), window, len(keys) > 0),
		CustomKeys: custom,
		SystemKeys: system,
		Truncated:  truncated,
	}, nil
}

// splitAttributeKeys separates the stored attribute paths into the custom keys
// integrations attached, spelled the way a filter names them (app.region is
// filtered as @region), and the system keys a search accepts.
func splitAttributeKeys(keys []string) ([]string, []string) {
	custom := []string{}
	system := []string{}
	for _, key := range keys {
		if suffix, ok := strings.CutPrefix(key, customAttributeStoragePath); ok && suffix != "" {
			custom = append(custom, customAttributePrefix+suffix)
			continue
		}
		// Content, header, and identity keys are withheld rather than listed: a
		// search refuses all three, so offering one would advertise a filter that
		// cannot be used, and listing the identity keys would suggest the audited
		// person filter can be sidestepped.
		if key == "" || isContentAttribute(key) || isHeaderAttribute(key) || isIdentityAttribute(key) {
			continue
		}
		system = append(system, key)
	}
	sort.Strings(custom)
	sort.Strings(system)
	return custom, system
}
