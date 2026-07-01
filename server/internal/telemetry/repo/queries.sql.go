package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// validJSONPath matches safe dot-separated JSON paths for ClickHouse attribute access.
// This prevents SQL injection since attribute paths cannot be parameterized.
//
//	^              - start of string
//	@?             - optional @ prefix (user attribute marker, translated to "app." prefix)
//	[a-zA-Z_]      - first segment char must be a letter or underscore
//	[a-zA-Z0-9_]*  - rest of first segment: letters, digits, underscores
//	(\.[a-zA-Z_][a-zA-Z0-9_]*)* - additional dot-separated segments
//	$              - end of string
//
// Matches: "@user.region", "http.route", "env"
// Rejects: "1bad", ".leading.dot", "path with spaces", "semi;colon", "@@double", "trailing.", "double..dot"
var validJSONPath = regexp.MustCompile(`^@?[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)*$`)

const MaxClaudePromptCorrelationEditDistanceBytes = 65536

func userIdentifierExpr(col string) string {
	return "if(telemetry_logs." + col + " != '', telemetry_logs." + col + ", telemetry_logs.user_email)"
}

// withAccountTypeFilter applies the shared account-type filter semantics:
// "personal" is an exact match, while "team" matches everything not personal —
// unclassified (empty) rows count as team so the team view keeps
// pre-classification and non-attributed data (same convention as the chats
// list filter and the schema comment on attribute_metrics_summaries). An empty
// filter is a no-op.
func withAccountTypeFilter(sb squirrel.SelectBuilder, accountType string) squirrel.SelectBuilder {
	switch accountType {
	case "":
		return sb
	case "team":
		// ifNull is a no-op on the non-nullable String columns and keeps
		// NULL-projected CTE paths counting as team.
		return sb.Where("ifNull(account_type, '') != 'personal'")
	default:
		return sb.Where(squirrel.Eq{"account_type": accountType})
	}
}

// SearchUsers powers employee enrollment lists, so internal users are grouped by
// email first to collapse rows that mix email-only and opaque user.id identity.
// Rows that carry a user_id but no email resolve an email through the
// known_emails join (see SearchUsers) so a person's email-less rows (e.g. tool
// calls attributed by id only) merge into their email-keyed summary instead of
// splitting into a second, token-less one.
func searchUsersGroupExpr(groupBy string) string {
	if groupBy == "external_user_id" {
		return userIdentifierExpr("external_user_id")
	}
	return "multiIf(" +
		"telemetry_logs.user_email != '', telemetry_logs.user_email, " +
		"known_emails.known_email != '', known_emails.known_email, " +
		"telemetry_logs.user_id)"
}

// searchUsersKnownEmailsJoin maps each user_id to an email observed alongside it
// anywhere in the search window, for use as a LEFT JOIN on telemetry_logs. It
// backs the email fallback in searchUsersGroupExpr and takes three args:
// project id, time start, time end.
const searchUsersKnownEmailsJoin = "(SELECT user_id, any(user_email) AS known_email" +
	" FROM telemetry_logs" +
	" WHERE gram_project_id = ? AND time_unix_nano >= ? AND time_unix_nano <= ? AND user_id != '' AND user_email != ''" +
	" GROUP BY user_id) AS known_emails ON telemetry_logs.user_id = known_emails.user_id"

// totalTokensExpr is a grouped-aggregate expression that yields a reliable total
// token count. AI-coding providers like Claude Code report
// gen_ai.usage.input_tokens and gen_ai.usage.output_tokens but never emit
// gen_ai.usage.total_tokens, so rows that lack an explicit total fall back to
// input + output. Rows that do carry an explicit total keep using it (our native
// chat completions already set total = input + output), so the two row shapes
// stay consistent. Without the fallback, Claude Code sessions surface "0 tokens"
// in the costs/session views (DNO-323).
const totalTokensExpr = "sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') + " +
	"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)) + toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.total_tokens) = '')"

// AttributeFilter represents a filter on an arbitrary JSON attribute path.
// Paths prefixed with @ target user-defined attributes (translated to app.<path> in ClickHouse).
// Bare paths target system/OTel attributes directly.
type AttributeFilter struct {
	Path   string   // Attribute path, optionally @-prefixed (e.g. "@user.region", "http.route")
	Op     string   // Comparison operator: "eq", "not_eq", "contains", "exists", "not_exists", "in"
	Values []string // Values to compare against. One value for single-value ops, multiple for "in".
}

// Predicate returns the squirrel condition for this filter, or nil if the filter
// should be skipped (e.g. an operator that requires values but none were provided).
func (f AttributeFilter) Predicate(col string) squirrel.Sqlizer {
	if len(f.Values) == 0 && f.Op != "exists" && f.Op != "not_exists" {
		return nil
	}
	switch f.Op {
	case "eq", "":
		return squirrel.Expr(fmt.Sprintf("%s = ?", col), f.Values[0])
	case "not_eq":
		return squirrel.Expr(fmt.Sprintf("%s != ?", col), f.Values[0])
	case "contains":
		return squirrel.Expr(fmt.Sprintf("position(%s, ?) > 0", col), f.Values[0])
	case "in":
		return squirrel.Eq{col: f.Values}
	case "exists":
		return squirrel.Expr(fmt.Sprintf("%s != ''", col))
	case "not_exists":
		return squirrel.Expr(fmt.Sprintf("%s = ''", col))
	default:
		return squirrel.Expr(fmt.Sprintf("%s = ?", col), f.Values[0])
	}
}

// resolveAttributeColumn maps an AttributeFilter.Path to the ClickHouse column
// expression used in WHERE clauses.
//
//   - @-prefixed paths → toString(attributes.app.<path>) (user attributes)
//   - Materialized column hit → bare column name (bloom-filter indexed)
//   - Fallback → toString(attributes.<path>) (JSON accessor)
func resolveAttributeColumn(path string) string {
	switch {
	case strings.HasPrefix(path, "@"):
		return fmt.Sprintf("toString(attributes.app.%s)", path[1:])
	case materializedColumns[path] != "":
		return materializedColumns[path]
	default:
		return fmt.Sprintf("toString(attributes.%s)", path)
	}
}

// toolUsageHTTPStatusPath is the attribute path the Tool Logs UI uses to filter by
// HTTP response status ("Non-2xx responses" etc.). A trace's status is a per-trace
// aggregate (the max status code across all of the trace's rows), so this filter must
// be applied to the aggregated normalized_traces column rather than pushed down to
// individual telemetry_logs rows. Pushed down, a "!= 200" predicate is satisfied by any
// status-less row of an otherwise-successful trace (e.g. a hook row that carries no
// http.response.status_code, whose stringified attribute is empty and so is trivially
// unequal to "200"), leaking the whole trace back into the result set after grouping
// where it shows up as a success/200. See DNO-447.
const toolUsageHTTPStatusPath = "http.response.status_code"

// toolUsageStatusPredicate builds a trace-level predicate on the aggregated
// http_status_code column (Nullable(Int32)) for an http.response.status_code filter.
// Comparisons are numeric so they match the max-status-code aggregation the query uses
// for the trace's status. Returns nil when the filter carries no usable value (numeric
// ops whose values don't parse as integers are skipped rather than erroring the query).
func toolUsageStatusPredicate(f AttributeFilter) squirrel.Sqlizer {
	const col = "http_status_code"
	switch f.Op {
	case "exists":
		return squirrel.Expr(col + " IS NOT NULL")
	case "not_exists":
		return squirrel.Expr(col + " IS NULL")
	case "contains":
		if len(f.Values) == 0 {
			return nil
		}
		return squirrel.Expr("position(toString("+col+"), ?) > 0", f.Values[0])
	}

	// Remaining ops (eq, not_eq, in, and the default) compare numerically.
	codes := make([]int32, 0, len(f.Values))
	for _, v := range f.Values {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			continue
		}
		codes = append(codes, int32(n)) //nolint:gosec // HTTP status codes are small
	}
	if len(codes) == 0 {
		return nil
	}
	switch f.Op {
	case "not_eq":
		return squirrel.Expr(col+" != ?", codes[0])
	case "in":
		return squirrel.Eq{col: codes}
	default: // eq and fallback
		return squirrel.Expr(col+" = ?", codes[0])
	}
}

// toolUsageOutcomePredicate builds a trace-level predicate for the first-class Tool Logs
// "Status" filter. It ORs together the selected outcomes, evaluated against the
// aggregated per-trace hook_status (Nullable String) and http_status_code (Nullable
// Int32) columns projected by both normalized_traces paths. The mapping mirrors the
// dashboard badge logic (getStatusConfig): hook_status wins when present, otherwise the
// HTTP status code decides. Unknown/empty selections yield nil so the query is
// unaffected.
func toolUsageOutcomePredicate(statuses []string) squirrel.Sqlizer {
	or := squirrel.Or{}
	for _, status := range statuses {
		switch status {
		case "blocked":
			or = append(or, squirrel.Expr("hook_status = 'blocked'"))
		case "failure", "error":
			or = append(or, squirrel.Expr("(hook_status = 'failure' OR (hook_status IS NULL AND http_status_code >= 400))"))
		case "success":
			or = append(or, squirrel.Expr("(hook_status = 'success' OR (hook_status IS NULL AND http_status_code >= 200 AND http_status_code < 400))"))
		case "pending":
			or = append(or, squirrel.Expr("hook_status = 'pending'"))
		}
	}
	if len(or) == 0 {
		return nil
	}
	return or
}

// sq is the squirrel statement builder pre-configured for ClickHouse (uses ? placeholders).
var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// applyHookFiltersToBuilder applies attribute filters and hook type conditions to sb.
// It mirrors the filter logic used in ListHooksTraces.
func applyHookFiltersToBuilder(sb squirrel.SelectBuilder, filters []AttributeFilter, typesToInclude []string) squirrel.SelectBuilder {
	for _, filter := range filters {
		if !validJSONPath.MatchString(filter.Path) {
			continue // skip invalid paths to prevent injection
		}
		col := resolveAttributeColumn(filter.Path)
		pred := filter.Predicate(col)
		if pred != nil {
			sb = sb.Where(pred)
		}
	}
	if len(typesToInclude) > 0 {
		typeConditions := make([]string, 0, len(typesToInclude))
		for _, hookType := range typesToInclude {
			switch hookType {
			case "skill":
				typeConditions = append(typeConditions, "tool_name = 'Skill'")
			case "mcp":
				typeConditions = append(typeConditions, "(tool_source != '' AND tool_name != 'Skill')")
			case "local":
				typeConditions = append(typeConditions, "(tool_source = '' AND tool_name != 'Skill')")
			}
		}
		if len(typeConditions) > 0 {
			sb = sb.Where(fmt.Sprintf("(%s)", strings.Join(typeConditions, " OR ")))
		}
	}
	return sb
}

// InsertTelemetryLogParams contains the parameters for inserting a telemetry log.
type InsertTelemetryLogParams struct {
	ID                   string
	TimeUnixNano         int64
	ObservedTimeUnixNano int64
	SeverityText         *string
	Body                 string
	TraceID              *string
	SpanID               *string
	Attributes           string
	ResourceAttributes   string
	GramProjectID        string
	GramDeploymentID     *string
	GramFunctionID       *string
	GramURN              string
	ServiceName          string
	ServiceVersion       *string
	GramChatID           *string
}

// InsertTelemetryLog inserts a telemetry log record into ClickHouse.
//
// Original SQL reference:
// INSERT INTO telemetry_logs (id, time_unix_nano, ...) VALUES (?, ?, ...)
func (q *Queries) InsertTelemetryLog(ctx context.Context, arg InsertTelemetryLogParams) error {
	return q.InsertTelemetryLogs(ctx, []InsertTelemetryLogParams{arg})
}

// InsertTelemetryLogs inserts telemetry log records using a server-side async
// insert (async_insert=1, wait_for_async_insert=0). The call is fire-and-forget
// from CH's perspective: it acks once the rows are queued in CH's async insert
// buffer, not once they are committed to disk.
func (q *Queries) InsertTelemetryLogs(ctx context.Context, args []InsertTelemetryLogParams) error {
	return q.insertTelemetryLogsInto(ctx, "telemetry_logs", args)
}

type UpsertShadowMCPInventoryURLParams struct {
	GramProjectID      string
	CanonicalServerURL string
	URLHost            string
	ServerName         string
	SeenAt             time.Time
	FirstSeen          time.Time
	LastSeen           time.Time
	UpdatedAt          time.Time
}

type ListShadowMCPInventoryURLsParams struct {
	GramProjectID string
	Limit         int
	Cursor        string
}

type ShadowMCPInventoryURLRow struct {
	CanonicalServerURL string    `ch:"canonical_server_url"`
	URLHost            string    `ch:"url_host"`
	ServerName         string    `ch:"server_name"`
	FirstSeen          time.Time `ch:"first_seen"`
	LastSeen           time.Time `ch:"last_seen"`
}

type ListShadowMCPInventoryUsageParams struct {
	GramProjectID       string
	CanonicalServerURLs []string
	Limit               int
}

type ShadowMCPInventoryUsageRow struct {
	CanonicalServerURL string
	ServerName         string
	FirstCalled        *time.Time
	LastCalled         *time.Time
	CallCount          uint64
	UserCount          uint64
	TopUsers           []string
}

type ListShadowMCPInventoryUsersParams struct {
	GramProjectID      string
	CanonicalServerURL string
	Limit              int
}

type ShadowMCPInventoryUserRow struct {
	UserKey    string
	LastCalled time.Time
	CallCount  uint64
}

type shadowMCPInventoryTraceUsageRow struct {
	TraceID    string    `ch:"trace_id"`
	ServerURL  string    `ch:"server_url"`
	ServerName string    `ch:"server_name"`
	UserKey    string    `ch:"user_key"`
	CalledAt   time.Time `ch:"called_at"`
}

type shadowMCPInventoryURLUpsert struct {
	GramProjectID      string
	CanonicalServerURL string
	URLHost            string
	ServerName         string
	FirstSeen          time.Time
	LastSeen           time.Time
	UpdatedAt          time.Time
}

type shadowMCPInventoryURLCursor struct {
	CanonicalServerURL string `json:"canonical_server_url"`
	LastSeenUnixNano   int64  `json:"last_seen_unix_nano"`
}

func EncodeShadowMCPInventoryURLCursor(row ShadowMCPInventoryURLRow) (string, error) {
	payload := shadowMCPInventoryURLCursor{
		CanonicalServerURL: row.CanonicalServerURL,
		LastSeenUnixNano:   row.LastSeen.UTC().UnixNano(),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encoding shadow mcp inventory url cursor: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeShadowMCPInventoryURLCursor(cursor string) (shadowMCPInventoryURLCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return shadowMCPInventoryURLCursor{}, fmt.Errorf("decoding shadow mcp inventory url cursor: %w", err)
	}

	var payload shadowMCPInventoryURLCursor
	if err := json.Unmarshal(data, &payload); err != nil {
		return shadowMCPInventoryURLCursor{}, fmt.Errorf("parsing shadow mcp inventory url cursor: %w", err)
	}
	if payload.CanonicalServerURL == "" {
		return shadowMCPInventoryURLCursor{}, fmt.Errorf("shadow mcp inventory url cursor canonical server url is required")
	}
	if payload.LastSeenUnixNano == 0 {
		return shadowMCPInventoryURLCursor{}, fmt.Errorf("shadow mcp inventory url cursor last seen is required")
	}

	return payload, nil
}

// UpsertShadowMCPInventoryURLs merges the given rows with any existing
// inventory rows and writes them using a server-side async insert
// (fire-and-forget), same as InsertTelemetryLogs.
func (q *Queries) UpsertShadowMCPInventoryURLs(ctx context.Context, args []UpsertShadowMCPInventoryURLParams) error {
	if len(args) == 0 {
		return nil
	}

	upserts := make(map[string]*shadowMCPInventoryURLUpsert, len(args))
	for _, arg := range args {
		if arg.GramProjectID == "" || arg.CanonicalServerURL == "" {
			continue
		}
		seenAt := arg.SeenAt
		if seenAt.IsZero() {
			seenAt = time.Now()
		}
		firstSeen := arg.FirstSeen
		if firstSeen.IsZero() {
			firstSeen = seenAt
		}
		lastSeen := arg.LastSeen
		if lastSeen.IsZero() {
			lastSeen = seenAt
		}
		if lastSeen.Before(firstSeen) {
			firstSeen, lastSeen = lastSeen, firstSeen
		}
		updatedAt := arg.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = time.Now()
		}

		key := arg.GramProjectID + "\x00" + arg.CanonicalServerURL
		upsert := upserts[key]
		if upsert == nil {
			upsert = &shadowMCPInventoryURLUpsert{
				GramProjectID:      arg.GramProjectID,
				CanonicalServerURL: arg.CanonicalServerURL,
				URLHost:            arg.URLHost,
				ServerName:         arg.ServerName,
				FirstSeen:          firstSeen.UTC(),
				LastSeen:           lastSeen.UTC(),
				UpdatedAt:          updatedAt.UTC(),
			}
			upserts[key] = upsert
			continue
		}
		if upsert.URLHost == "" {
			upsert.URLHost = arg.URLHost
		}
		if arg.ServerName != "" {
			upsert.ServerName = arg.ServerName
		}
		if firstSeen.Before(upsert.FirstSeen) {
			upsert.FirstSeen = firstSeen.UTC()
		}
		if lastSeen.After(upsert.LastSeen) {
			upsert.LastSeen = lastSeen.UTC()
		}
		if updatedAt.After(upsert.UpdatedAt) {
			upsert.UpdatedAt = updatedAt.UTC()
		}
	}

	if len(upserts) == 0 {
		return nil
	}

	for _, upsert := range upserts {
		existing, err := q.getShadowMCPInventoryURL(ctx, upsert.GramProjectID, upsert.CanonicalServerURL)
		if err != nil {
			return err
		}
		if existing == nil {
			continue
		}
		if upsert.URLHost == "" {
			upsert.URLHost = existing.URLHost
		}
		if upsert.ServerName == "" {
			upsert.ServerName = existing.ServerName
		}
		if existing.FirstSeen.Before(upsert.FirstSeen) {
			upsert.FirstSeen = existing.FirstSeen
		}
		if existing.LastSeen.After(upsert.LastSeen) {
			upsert.LastSeen = existing.LastSeen
		}
	}

	ctx = clickhouse.Context(ctx, clickhouse.WithAsync(false))
	builder := sq.Insert("shadow_mcp_inventory_urls").
		Columns(
			"gram_project_id",
			"canonical_server_url",
			"url_host",
			"server_name",
			"first_seen",
			"last_seen",
			"updated_at",
		)

	for _, upsert := range upserts {
		builder = builder.Values(
			upsert.GramProjectID,
			upsert.CanonicalServerURL,
			upsert.URLHost,
			upsert.ServerName,
			upsert.FirstSeen.UTC(),
			upsert.LastSeen.UTC(),
			upsert.UpdatedAt.UTC(),
		)
	}

	query, queryArgs, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("building shadow mcp inventory url upsert query: %w", err)
	}

	if err := q.conn.Exec(ctx, query, queryArgs...); err != nil {
		return fmt.Errorf("upserting shadow mcp inventory urls: %w", err)
	}

	return nil
}

func (q *Queries) getShadowMCPInventoryURL(ctx context.Context, projectID string, canonicalURL string) (*ShadowMCPInventoryURLRow, error) {
	sb := sq.Select(
		"canonical_server_url",
		"max(url_host) AS url_host",
		"argMaxIf(server_name, updated_at, server_name != '') AS server_name",
		"min(first_seen) AS first_seen",
		"max(last_seen) AS last_seen",
	).
		From("shadow_mcp_inventory_urls").
		Where("gram_project_id = ?", projectID).
		Where("canonical_server_url = ?", canonicalURL).
		GroupBy("gram_project_id", "canonical_server_url").
		Limit(1)

	query, queryArgs, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building shadow mcp inventory url lookup query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying shadow mcp inventory url lookup: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterating shadow mcp inventory url lookup rows: %w", err)
		}
		return nil, nil
	}

	var row ShadowMCPInventoryURLRow
	if err := rows.ScanStruct(&row); err != nil {
		return nil, fmt.Errorf("scanning shadow mcp inventory url lookup row: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating shadow mcp inventory url lookup rows: %w", err)
	}

	return &row, nil
}

func (q *Queries) ListShadowMCPInventoryURLs(ctx context.Context, arg ListShadowMCPInventoryURLsParams) ([]ShadowMCPInventoryURLRow, error) {
	limit := clampShadowMCPInventoryLimit(arg.Limit)
	var cursor shadowMCPInventoryURLCursor
	if arg.Cursor != "" {
		var err error
		cursor, err = decodeShadowMCPInventoryURLCursor(arg.Cursor)
		if err != nil {
			return nil, err
		}
	}

	inventoryRows := sq.Select(
		"canonical_server_url",
		"max(url_host) AS url_host",
		"argMaxIf(server_name, updated_at, server_name != '') AS server_name",
		"min(first_seen) AS first_seen",
		"max(last_seen) AS last_seen",
	).
		From("shadow_mcp_inventory_urls").
		Where("gram_project_id = ?", arg.GramProjectID).
		GroupBy("gram_project_id", "canonical_server_url")

	sb := sq.Select(
		"canonical_server_url",
		"url_host",
		"server_name",
		"first_seen",
		"last_seen",
	).
		FromSelect(inventoryRows, "inventory_urls").
		Limit(limit)

	if arg.Cursor != "" {
		lastSeen := time.Unix(0, cursor.LastSeenUnixNano).UTC()
		sb = sb.Where(squirrel.Or{
			squirrel.Expr("last_seen < ?", lastSeen),
			squirrel.And{
				squirrel.Expr("last_seen = ?", lastSeen),
				squirrel.Expr("canonical_server_url > ?", cursor.CanonicalServerURL),
			},
		})
	}

	sb = sb.OrderBy("last_seen DESC", "canonical_server_url ASC")

	query, queryArgs, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building shadow mcp inventory url query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying shadow mcp inventory urls: %w", err)
	}
	defer func() { _ = rows.Close() }()

	inventoryURLs := make([]ShadowMCPInventoryURLRow, 0)
	for rows.Next() {
		var row ShadowMCPInventoryURLRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning shadow mcp inventory url row: %w", err)
		}
		inventoryURLs = append(inventoryURLs, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating shadow mcp inventory url rows: %w", err)
	}

	return inventoryURLs, nil
}

func (q *Queries) ListShadowMCPInventoryUsage(ctx context.Context, arg ListShadowMCPInventoryUsageParams) ([]ShadowMCPInventoryUsageRow, error) {
	traceRows, err := q.listShadowMCPInventoryTraceUsage(ctx, arg.GramProjectID, arg.CanonicalServerURLs, arg.Limit)
	if err != nil {
		return nil, err
	}

	canonicalURLSet := shadowMCPInventoryCanonicalURLSet(arg.CanonicalServerURLs)
	usageByURL := make(map[string]*ShadowMCPInventoryUsageRow)
	usersByURL := make(map[string]map[string]*ShadowMCPInventoryUserRow)
	for _, traceRow := range traceRows {
		invURL, ok := shadowmcp.CanonicalizeInventoryURL(traceRow.ServerURL)
		if !ok {
			continue
		}
		if len(canonicalURLSet) > 0 && !canonicalURLSet[invURL.CanonicalURL] {
			continue
		}
		usage := usageByURL[invURL.CanonicalURL]
		if usage == nil {
			firstCalled := traceRow.CalledAt
			lastCalled := traceRow.CalledAt
			usage = &ShadowMCPInventoryUsageRow{
				CanonicalServerURL: invURL.CanonicalURL,
				ServerName:         traceRow.ServerName,
				FirstCalled:        &firstCalled,
				LastCalled:         &lastCalled,
				CallCount:          0,
				UserCount:          0,
				TopUsers:           nil,
			}
			usageByURL[invURL.CanonicalURL] = usage
		}
		if traceRow.ServerName != "" {
			usage.ServerName = traceRow.ServerName
		}
		if usage.FirstCalled == nil || traceRow.CalledAt.Before(*usage.FirstCalled) {
			firstCalled := traceRow.CalledAt
			usage.FirstCalled = &firstCalled
		}
		if usage.LastCalled == nil || traceRow.CalledAt.After(*usage.LastCalled) {
			lastCalled := traceRow.CalledAt
			usage.LastCalled = &lastCalled
		}
		usage.CallCount++

		if traceRow.UserKey == "" {
			continue
		}
		users := usersByURL[invURL.CanonicalURL]
		if users == nil {
			users = make(map[string]*ShadowMCPInventoryUserRow)
			usersByURL[invURL.CanonicalURL] = users
		}
		user := users[traceRow.UserKey]
		if user == nil {
			user = &ShadowMCPInventoryUserRow{
				UserKey:    traceRow.UserKey,
				LastCalled: traceRow.CalledAt,
				CallCount:  0,
			}
			users[traceRow.UserKey] = user
		}
		user.CallCount++
		if traceRow.CalledAt.After(user.LastCalled) {
			user.LastCalled = traceRow.CalledAt
		}
	}

	usageRows := make([]ShadowMCPInventoryUsageRow, 0, len(usageByURL))
	for canonicalURL, usage := range usageByURL {
		users := sortedShadowMCPInventoryUsers(usersByURL[canonicalURL])
		usage.UserCount = uint64(len(users))
		topUsers := make([]string, 0, min(len(users), 5))
		for i := 0; i < len(users) && i < 5; i++ {
			topUsers = append(topUsers, users[i].UserKey)
		}
		usage.TopUsers = topUsers
		usageRows = append(usageRows, *usage)
	}
	sort.Slice(usageRows, func(i, j int) bool {
		return usageRows[i].CanonicalServerURL < usageRows[j].CanonicalServerURL
	})

	return usageRows, nil
}

func (q *Queries) ListShadowMCPInventoryUsers(ctx context.Context, arg ListShadowMCPInventoryUsersParams) ([]ShadowMCPInventoryUserRow, error) {
	traceRows, err := q.listShadowMCPInventoryTraceUsage(ctx, arg.GramProjectID, []string{arg.CanonicalServerURL}, arg.Limit)
	if err != nil {
		return nil, err
	}

	users := make(map[string]*ShadowMCPInventoryUserRow)
	for _, traceRow := range traceRows {
		invURL, ok := shadowmcp.CanonicalizeInventoryURL(traceRow.ServerURL)
		if !ok || invURL.CanonicalURL != arg.CanonicalServerURL || traceRow.UserKey == "" {
			continue
		}
		user := users[traceRow.UserKey]
		if user == nil {
			user = &ShadowMCPInventoryUserRow{
				UserKey:    traceRow.UserKey,
				LastCalled: traceRow.CalledAt,
				CallCount:  0,
			}
			users[traceRow.UserKey] = user
		}
		user.CallCount++
		if traceRow.CalledAt.After(user.LastCalled) {
			user.LastCalled = traceRow.CalledAt
		}
	}

	userRows := sortedShadowMCPInventoryUsers(users)
	limit := clampShadowMCPInventoryLimitInt(arg.Limit)
	if len(userRows) > limit {
		userRows = userRows[:limit]
	}
	return userRows, nil
}

func (q *Queries) listShadowMCPInventoryTraceUsage(ctx context.Context, projectID string, canonicalServerURLs []string, limit int) ([]shadowMCPInventoryTraceUsageRow, error) {
	sb := sq.Select(
		"trace_id",
		"max(mcp_server_url) AS server_url",
		"max(tool_source) AS server_name",
		"if(max(user_email) != '', max(user_email), max(user_id)) AS user_key",
		"fromUnixTimestamp64Nano(max(start_time_unix_nano)) AS called_at",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", projectID).
		GroupBy("trace_id").
		Having("server_url != ''").
		OrderBy("max(start_time_unix_nano) DESC", "trace_id ASC")

	if len(canonicalServerURLs) > 0 {
		predicates := make(squirrel.Or, 0, len(canonicalServerURLs))
		for _, canonicalURL := range canonicalServerURLs {
			if canonicalURL == "" {
				continue
			}
			predicates = append(predicates, squirrel.Or{
				squirrel.Expr("server_url = ?", canonicalURL),
				squirrel.Expr("startsWith(server_url, ?)", canonicalURL+"?"),
				squirrel.Expr("startsWith(server_url, ?)", canonicalURL+"#"),
			})
		}
		if len(predicates) > 0 {
			sb = sb.Having(predicates)
		}
	} else {
		sb = sb.Limit(clampShadowMCPInventoryUsageTraceLimit(limit))
	}

	query, queryArgs, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building shadow mcp inventory trace usage query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying shadow mcp inventory trace usage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	traceRows := make([]shadowMCPInventoryTraceUsageRow, 0)
	for rows.Next() {
		var row shadowMCPInventoryTraceUsageRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scanning shadow mcp inventory trace usage row: %w", err)
		}
		traceRows = append(traceRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating shadow mcp inventory trace usage rows: %w", err)
	}

	return traceRows, nil
}

func shadowMCPInventoryCanonicalURLSet(canonicalServerURLs []string) map[string]bool {
	if len(canonicalServerURLs) == 0 {
		return nil
	}
	out := make(map[string]bool, len(canonicalServerURLs))
	for _, canonicalURL := range canonicalServerURLs {
		if canonicalURL != "" {
			out[canonicalURL] = true
		}
	}
	return out
}

func sortedShadowMCPInventoryUsers(users map[string]*ShadowMCPInventoryUserRow) []ShadowMCPInventoryUserRow {
	userRows := make([]ShadowMCPInventoryUserRow, 0, len(users))
	for _, user := range users {
		userRows = append(userRows, *user)
	}
	sort.Slice(userRows, func(i, j int) bool {
		if userRows[i].CallCount != userRows[j].CallCount {
			return userRows[i].CallCount > userRows[j].CallCount
		}
		if !userRows[i].LastCalled.Equal(userRows[j].LastCalled) {
			return userRows[i].LastCalled.After(userRows[j].LastCalled)
		}
		return userRows[i].UserKey < userRows[j].UserKey
	})
	return userRows
}

func clampShadowMCPInventoryLimit(limit int) uint64 {
	return uint64(clampShadowMCPInventoryLimitInt(limit)) // #nosec G115 -- clamped to 1..500 by clampShadowMCPInventoryLimitInt.
}

func clampShadowMCPInventoryLimitInt(limit int) int {
	switch {
	case limit <= 0:
		return 50
	case limit > 500:
		return 500
	default:
		return limit
	}
}

func clampShadowMCPInventoryUsageTraceLimit(limit int) uint64 {
	switch {
	case limit <= 0:
		return 5000
	case limit > 50000:
		return 50000
	default:
		return uint64(limit)
	}
}

// ListTelemetryLogsParams contains the parameters for listing telemetry logs.
type ListTelemetryLogsParams struct {
	GramProjectID          string
	TimeStart              int64
	TimeEnd                int64
	GramURNs               []string // Supports multiple URNs
	TraceID                string
	GramDeploymentID       string
	GramFunctionID         string
	SeverityText           string
	HTTPResponseStatusCode int32
	HTTPRoute              string
	HTTPRequestMethod      string
	ServiceName            string
	GramChatID             string
	UserID                 string
	ExternalUserID         string
	EventSource            string
	AttributeFilters       []AttributeFilter
	SortOrder              string
	Cursor                 string
	Limit                  int
}

// ListTelemetryLogs retrieves telemetry logs with optional filters and cursor pagination.
//
// Original SQL reference:
// SELECT id, time_unix_nano, ... FROM telemetry_logs
// WHERE gram_project_id = ? AND time_unix_nano >= ? AND time_unix_nano <= ?
// [+ optional filters] ORDER BY time_unix_nano LIMIT ?
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListTelemetryLogs(ctx context.Context, arg ListTelemetryLogsParams) ([]TelemetryLog, error) {
	sb := sq.Select(
		"id",
		"time_unix_nano",
		"observed_time_unix_nano",
		"severity_text",
		"body",
		"trace_id",
		"span_id",
		"toString(attributes) as attributes",
		"toString(resource_attributes) as resource_attributes",
		"gram_project_id",
		"gram_deployment_id",
		"gram_function_id",
		"gram_urn",
		"service_name",
		"service_version",
		"gram_chat_id",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd)

	// Optional filters — use prefix matching so URN prefixes like "tools:http:gram"
	// match fully-qualified URNs like "tools:http:gram:my_tool".
	if len(arg.GramURNs) > 0 {
		sb = sb.Where("arrayExists(x -> startsWith(gram_urn, concat(x, ':')) OR gram_urn = x, ?)", arg.GramURNs)
	}
	if arg.TraceID != "" {
		sb = sb.Where(squirrel.Eq{"trace_id": arg.TraceID})
	}
	if arg.GramDeploymentID != "" {
		sb = sb.Where("gram_deployment_id = toUUIDOrNull(?)", arg.GramDeploymentID)
	}
	if arg.GramFunctionID != "" {
		sb = sb.Where("gram_function_id = toUUIDOrNull(?)", arg.GramFunctionID)
	}
	if arg.SeverityText != "" {
		sb = sb.Where(squirrel.Eq{"severity_text": arg.SeverityText})
	}
	if arg.HTTPResponseStatusCode != 0 {
		sb = sb.Where("toInt32OrZero(toString(attributes.http.response.status_code)) = ?", arg.HTTPResponseStatusCode)
	}
	if arg.HTTPRoute != "" {
		sb = sb.Where("toString(attributes.http.route) = ?", arg.HTTPRoute)
	}
	if arg.HTTPRequestMethod != "" {
		sb = sb.Where("toString(attributes.http.request.method) = ?", arg.HTTPRequestMethod)
	}
	if arg.ServiceName != "" {
		sb = sb.Where(squirrel.Eq{"service_name": arg.ServiceName})
	}
	if arg.GramChatID != "" {
		sb = sb.Where(squirrel.Eq{"gram_chat_id": arg.GramChatID})
	}
	if arg.UserID != "" {
		sb = sb.Where(squirrel.Eq{"user_id": arg.UserID})
	}
	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}
	if arg.EventSource != "" {
		sb = sb.Where(squirrel.Eq{"event_source": arg.EventSource})
	}

	// Arbitrary attribute filters
	for _, f := range arg.AttributeFilters {
		if !validJSONPath.MatchString(f.Path) {
			continue // skip invalid paths to prevent injection
		}
		pred := f.Predicate(resolveAttributeColumn(f.Path))
		if pred == nil {
			continue
		}
		sb = sb.Where(pred)
	}

	sb = withPagination(sb, arg.Cursor, arg.SortOrder)

	sb = withOrdering(sb, arg.SortOrder, "time_unix_nano", "toUUID(id)")

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list logs query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TelemetryLog
	for rows.Next() {
		var log TelemetryLog
		if err = rows.ScanStruct(&log); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		items = append(items, log)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

// ListToolTracesParams contains the parameters for listing tool call traces.
type ListToolTracesParams struct {
	GramProjectID    string
	TimeStart        int64
	TimeEnd          int64
	GramDeploymentID string
	GramFunctionID   string
	GramURN          string // Single URN filter (supports substring matching)
	EventSource      string
	SortOrder        string
	Cursor           string // trace_id to paginate from
	Limit            int
}

// ListToolTraces retrieves aggregated trace summaries for tool calls (filtered to only include traces with tool_name set).
//
// Original SQL reference:
// SELECT trace_id, min(time_unix_nano), count(*), ... FROM telemetry_logs
// WHERE gram_project_id = ? AND time_unix_nano >= ? AND time_unix_nano <= ?
// [+ optional filters] GROUP BY trace_id ORDER BY start_time_unix_nano LIMIT ?
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListToolTraces(ctx context.Context, arg ListToolTracesParams) ([]TraceSummary, error) {
	sb := sq.Select(
		"trace_id",
		"min(start_time_unix_nano) as start_time_unix_nano",
		"sum(log_count) as log_count",
		"anyIfMerge(http_status_code) as http_status_code",
		"any(gram_urn) as gram_urn",
		"any(tool_name) as tool_name",
		"any(tool_source) as tool_source",
		"any(event_source) as event_source",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Having("start_time_unix_nano >= ?", arg.TimeStart).
		Having("start_time_unix_nano <= ?", arg.TimeEnd)

	// Optional filters
	if arg.GramDeploymentID != "" {
		sb = sb.Where("gram_deployment_id = toUUIDOrNull(?)", arg.GramDeploymentID)
	}
	if arg.GramFunctionID != "" {
		sb = sb.Where("gram_function_id = toUUIDOrNull(?)", arg.GramFunctionID)
	}

	// Build HAVING clause for tool filtering.
	// IMPORTANT: We must construct a single HAVING clause with explicit AND logic to ensure
	// correct boolean precedence. Multiple .Having() calls would create separate conditions
	// that interact incorrectly with the OR in the tool_name check, causing the gram_urn
	// filter to be bypassed when startsWith(gram_urn, 'tools:') is true.
	havingParts := []string{"((tool_name IS NOT NULL AND tool_name != '') OR startsWith(gram_urn, 'tools:'))"}
	havingArgs := []any{}

	// URN filter must use HAVING because it's an aggregate function in SELECT
	if arg.GramURN != "" {
		havingParts = append(havingParts, "position(gram_urn, ?) > 0")
		havingArgs = append(havingArgs, arg.GramURN)
	}

	// EventSource filter must use HAVING because it's an aggregate function in SELECT
	if arg.EventSource != "" {
		havingParts = append(havingParts, "event_source = ?")
		havingArgs = append(havingArgs, arg.EventSource)
	} else {
		// Exclude hooks logs by default when no event_source filter is specified
		havingParts = append(havingParts, "event_source != ?")
		havingArgs = append(havingArgs, "hook")
	}

	// Combine all HAVING conditions with explicit AND to ensure proper filtering
	if len(havingParts) > 0 {
		sb = sb.Having(strings.Join(havingParts, " AND "), havingArgs...)
	}

	// Exclude chat completion logs (urn:uuid:...) which are not tool calls.
	// The trace_summaries_mv filters these at insert time via a WHERE clause,
	// so for new data any(gram_urn) will never pick a urn:uuid: value.
	// This HAVING clause is kept as a safety net for historical data that may
	// have been inserted before the MV was updated to exclude these URNs.
	sb = sb.Having("position(gram_urn, 'urn:uuid:') != 1")

	sb = sb.GroupBy("trace_id")

	sb = withHavingPagination(
		sb,
		arg.Cursor,
		arg.SortOrder,
		arg.GramProjectID,
		"trace_id",
		"start_time_unix_nano",
		"min(start_time_unix_nano)",
		TableNameTraceSummaries,
	)

	sb = withOrdering(sb, arg.SortOrder, "start_time_unix_nano", "")

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list tool traces query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var traces []TraceSummary
	for rows.Next() {
		var trace TraceSummary
		if err = rows.ScanStruct(&trace); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		traces = append(traces, trace)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return traces, nil
}

// GetMetricsSummaryParams contains the parameters for getting metrics summary.
type GetMetricsSummaryParams struct {
	GramProjectID string
	TimeStart     int64
	TimeEnd       int64
}

// GetMetricsSummary retrieves aggregate metrics for a project within a time range.
//
// Original SQL reference:
// SELECT [aggregation functions] FROM telemetry_logs
// WHERE gram_project_id = ? AND time_unix_nano >= ? AND time_unix_nano <= ?
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetMetricsSummary(ctx context.Context, arg GetMetricsSummaryParams) (*MetricsSummaryRow, error) {
	sb := sq.Select(
		// Activity timestamps
		"min(first_seen_unix_nano) AS first_seen_unix_nano",
		"max(last_seen_unix_nano) AS last_seen_unix_nano",

		// Cardinality
		"uniqExactIfMerge(total_chats) AS total_chats",
		"uniqExactIfMerge(distinct_models) AS distinct_models",
		"uniqExactIfMerge(distinct_providers) AS distinct_providers",

		// Token metrics
		"sumIfMerge(total_input_tokens) AS total_input_tokens",
		"sumIfMerge(total_output_tokens) AS total_output_tokens",
		"sumIfMerge(total_tokens) AS total_tokens",
		"avgIfMerge(avg_tokens_per_request) AS avg_tokens_per_request",

		// Chat request metrics
		"countIfMerge(total_chat_requests) AS total_chat_requests",
		"avgIfMerge(avg_chat_duration_ms) AS avg_chat_duration_ms",

		// Resolution status
		"countIfMerge(finish_reason_stop) AS finish_reason_stop",
		"countIfMerge(finish_reason_tool_calls) AS finish_reason_tool_calls",

		// Tool call metrics
		"countIfMerge(total_tool_calls) AS total_tool_calls",
		"countIfMerge(tool_call_success) AS tool_call_success",
		"countIfMerge(tool_call_failure) AS tool_call_failure",
		"avgIfMerge(avg_tool_duration_ms) AS avg_tool_duration_ms",

		// Chat resolution metrics
		"countIfMerge(chat_resolution_success) AS chat_resolution_success",
		"countIfMerge(chat_resolution_failure) AS chat_resolution_failure",
		"countIfMerge(chat_resolution_partial) AS chat_resolution_partial",
		"countIfMerge(chat_resolution_abandoned) AS chat_resolution_abandoned",
		"avgIfMerge(avg_chat_resolution_score) AS avg_chat_resolution_score",

		// Model breakdown
		"sumMapIfMerge(models) AS models",

		// Tool breakdowns
		"sumMapIfMerge(tool_counts) AS tool_counts",
		"sumMapIfMerge(tool_success_counts) AS tool_success_counts",
		"sumMapIfMerge(tool_failure_counts) AS tool_failure_counts",
	).
		From("metrics_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_bucket >= toStartOfHour(fromUnixTimestamp64Nano(?))", arg.TimeStart).
		Where("time_bucket <= toStartOfHour(fromUnixTimestamp64Nano(?))", arg.TimeEnd)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building metrics summary query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		// Return empty metrics if no rows
		return &MetricsSummaryRow{
			FirstSeenUnixNano:        0,
			LastSeenUnixNano:         0,
			TotalChats:               0,
			DistinctModels:           0,
			DistinctProviders:        0,
			TotalInputTokens:         0,
			TotalOutputTokens:        0,
			TotalTokens:              0,
			CacheReadInputTokens:     0,
			CacheCreationInputTokens: 0,
			AvgTokensPerReq:          0,
			TotalCost:                0,
			TotalChatRequests:        0,
			AvgChatDurationMs:        0,
			FinishReasonStop:         0,
			FinishReasonToolCalls:    0,
			TotalToolCalls:           0,
			ToolCallSuccess:          0,
			ToolCallFailure:          0,
			AvgToolDurationMs:        0,
			ChatResolutionSuccess:    0,
			ChatResolutionFailure:    0,
			ChatResolutionPartial:    0,
			ChatResolutionAbandoned:  0,
			AvgChatResolutionScore:   0,
			Models:                   make(map[string]uint64),
			ToolCounts:               make(map[string]uint64),
			ToolSuccessCounts:        make(map[string]uint64),
			ToolFailureCounts:        make(map[string]uint64),
		}, nil
	}

	var metrics MetricsSummaryRow
	if err = rows.ScanStruct(&metrics); err != nil {
		return nil, fmt.Errorf("error scanning row: %w", err)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return &metrics, nil
}

// GetTimeSeriesMetricsParams contains the parameters for getting time series metrics.
type GetTimeSeriesMetricsParams struct {
	GramProjectID     string
	TimeStart         int64
	TimeEnd           int64
	IntervalSeconds   int64  // Bucket interval in seconds
	UserID            string // Optional filter
	ExternalUserID    string // Optional filter
	APIKeyID          string // Optional filter
	ToolsetSlug       string // Optional filter - filters by toolset/MCP server slug
	RemoteMCPServerID string // Optional filter - filters by remote_mcp_server_id
	MCPServerID       string // Optional filter - filters by mcp_server_id
	EventSource       string // Optional filter - filters by event_source
	HookSource        string // Optional filter - filters by hook_source
	AccountType       string // Optional filter - filters by account_type
	ExternalOrgID     string // Optional filter - scopes to a single account by provider org id
}

// GetTimeSeriesMetrics retrieves time-bucketed metrics for the observability overview charts.
// Returns buckets for the entire requested time range, with zeros for periods without data.
// Gap-filling is handled by ClickHouse's ORDER BY ... WITH FILL clause.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetTimeSeriesMetrics(ctx context.Context, arg GetTimeSeriesMetricsParams) ([]TimeSeriesBucket, error) {
	intervalNanos := arg.IntervalSeconds * 1_000_000_000
	// Align boundaries to interval so WITH FILL produces evenly-spaced buckets.
	alignedStart := (arg.TimeStart / intervalNanos) * intervalNanos
	// Add one step so WITH FILL's exclusive TO boundary includes the last aligned bucket.
	alignedEnd := ((arg.TimeEnd / intervalNanos) * intervalNanos) + intervalNanos

	// toIntervalSecond(?) allows the interval to be fully parameterized — unlike INTERVAL literals.
	sb := sq.Select().
		Column(squirrel.Expr(
			"toInt64(toStartOfInterval(fromUnixTimestamp64Nano(time_unix_nano), toIntervalSecond(?))) * 1000000000 AS bucket_time_unix_nano",
			arg.IntervalSeconds,
		)).
		Columns(
			"uniqExactIf(chat_id, chat_id != '') AS total_chats",
			"uniqExactIf(chat_id, chat_id != '' AND evaluation_score_label = 'success') AS resolved_chats",
			"uniqExactIf(chat_id, chat_id != '' AND evaluation_score_label = 'failure') AS failed_chats",
			"uniqExactIf(chat_id, chat_id != '' AND evaluation_score_label = 'partial') AS partial_chats",
			"uniqExactIf(chat_id, chat_id != '' AND evaluation_score_label = 'abandoned') AS abandoned_chats",
			"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') AS total_input_tokens",
			"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') AS total_output_tokens",
			"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens",
			"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '') AS cache_read_input_tokens",
			"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '') AS cache_creation_input_tokens",
			"sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') AS total_cost",
			"countIf(startsWith(gram_urn, 'tools:')) AS total_tool_calls",
			"countIf(startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS failed_tool_calls",
			"if(isNaN(avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(gram_urn, 'tools:'))), 0, avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(gram_urn, 'tools:'))) AS avg_tool_latency_ms",
			"if(isNaN(avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gen_ai.conversation.duration) != '')), 0, avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gen_ai.conversation.duration) != '')) AS avg_session_duration_ms",
		).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		GroupBy("bucket_time_unix_nano")

	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{userIdentifierExpr("external_user_id"): arg.ExternalUserID})
	}
	if arg.UserID != "" {
		sb = sb.Where(squirrel.Eq{userIdentifierExpr("user_id"): arg.UserID})
	}
	if arg.APIKeyID != "" {
		sb = sb.Where(squirrel.Eq{"api_key_id": arg.APIKeyID})
	}
	if arg.ToolsetSlug != "" {
		sb = sb.Where(squirrel.Eq{"toolset_slug": arg.ToolsetSlug})
	}
	if arg.RemoteMCPServerID != "" {
		sb = sb.Where(squirrel.Eq{"remote_mcp_server_id": arg.RemoteMCPServerID})
	}
	if arg.MCPServerID != "" {
		sb = sb.Where(squirrel.Eq{"mcp_server_id": arg.MCPServerID})
	}
	if arg.EventSource != "" {
		sb = sb.Where(squirrel.Eq{"event_source": arg.EventSource})
	}
	if arg.HookSource != "" {
		sb = sb.Where(squirrel.Eq{"hook_source": arg.HookSource})
	}
	sb = withAccountTypeFilter(sb, arg.AccountType)
	if arg.ExternalOrgID != "" {
		sb = sb.Where(squirrel.Eq{"external_org_id": arg.ExternalOrgID})
	}

	// ClickHouse fills missing buckets with zeros via WITH FILL.
	// FROM/TO use aligned nanosecond boundaries; TO is exclusive so we add one step.
	sb = sb.OrderByClause(squirrel.Expr(
		"bucket_time_unix_nano ASC WITH FILL FROM ? TO ? STEP ?",
		alignedStart, alignedEnd, intervalNanos,
	))

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building time series query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []TimeSeriesBucket
	for rows.Next() {
		var bucket TimeSeriesBucket
		if err = rows.ScanStruct(&bucket); err != nil {
			return nil, fmt.Errorf("scanning time series row: %w", err)
		}
		buckets = append(buckets, bucket)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return buckets, nil
}

// GetToolMetricsBreakdownParams contains the parameters for getting tool metrics breakdown.
type GetToolMetricsBreakdownParams struct {
	GramProjectID     string
	TimeStart         int64
	TimeEnd           int64
	UserID            string // Optional filter
	ExternalUserID    string // Optional filter
	APIKeyID          string // Optional filter
	ToolsetSlug       string // Optional filter - filters by toolset/MCP server slug
	RemoteMCPServerID string // Optional filter - filters by remote_mcp_server_id
	MCPServerID       string // Optional filter - filters by mcp_server_id
	EventSource       string // Optional filter - filters by event_source
	HookSource        string // Optional filter - filters by hook_source
	AccountType       string // Optional filter - filters by account_type
	ExternalOrgID     string // Optional filter - scopes to a single account by provider org id
	Limit             int
	SortBy            string // "count" or "failure_rate"
}

// GetToolMetricsBreakdown retrieves per-tool aggregated metrics for top tools tables.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetToolMetricsBreakdown(ctx context.Context, arg GetToolMetricsBreakdownParams) ([]ToolMetric, error) {
	sb := sq.Select(
		"gram_urn",
		"count(*) as call_count",
		"countIf(toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) as success_count",
		"countIf(toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) as failure_count",
		"avg(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000) as avg_latency_ms",
		"countIf(toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) / greatest(toFloat64(count(*)), 1) as failure_rate",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where("startsWith(gram_urn, 'tools:')")

	// Optional filters
	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{userIdentifierExpr("external_user_id"): arg.ExternalUserID})
	}
	if arg.UserID != "" {
		sb = sb.Where(squirrel.Eq{userIdentifierExpr("user_id"): arg.UserID})
	}
	if arg.APIKeyID != "" {
		sb = sb.Where(squirrel.Eq{"api_key_id": arg.APIKeyID})
	}
	if arg.ToolsetSlug != "" {
		sb = sb.Where(squirrel.Eq{"toolset_slug": arg.ToolsetSlug})
	}
	if arg.RemoteMCPServerID != "" {
		sb = sb.Where(squirrel.Eq{"remote_mcp_server_id": arg.RemoteMCPServerID})
	}
	if arg.MCPServerID != "" {
		sb = sb.Where(squirrel.Eq{"mcp_server_id": arg.MCPServerID})
	}
	if arg.EventSource != "" {
		sb = sb.Where(squirrel.Eq{"event_source": arg.EventSource})
	}
	if arg.HookSource != "" {
		sb = sb.Where(squirrel.Eq{"hook_source": arg.HookSource})
	}
	sb = withAccountTypeFilter(sb, arg.AccountType)
	if arg.ExternalOrgID != "" {
		sb = sb.Where(squirrel.Eq{"external_org_id": arg.ExternalOrgID})
	}

	sb = sb.GroupBy("gram_urn")

	// Sort by count or failure rate
	if arg.SortBy == "failure_rate" {
		sb = sb.OrderBy("failure_rate DESC", "call_count DESC")
	} else {
		sb = sb.OrderBy("call_count DESC")
	}

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool metrics query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tools []ToolMetric
	for rows.Next() {
		var tool ToolMetric
		if err = rows.ScanStruct(&tool); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		tools = append(tools, tool)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return tools, nil
}

// GetOverviewSummaryParams contains the parameters for getting overview summary metrics.
type GetOverviewSummaryParams struct {
	GramProjectID     string
	TimeStart         int64
	TimeEnd           int64
	UserID            string // Optional filter
	ExternalUserID    string // Optional filter
	APIKeyID          string // Optional filter
	ToolsetSlug       string // Optional filter - filters by toolset/MCP server slug
	RemoteMCPServerID string // Optional filter - filters by remote_mcp_server_id
	MCPServerID       string // Optional filter - filters by mcp_server_id
	EventSource       string // Optional filter - filters by event_source
	HookSource        string // Optional filter - filters by hook_source
	AccountType       string // Optional filter - filters by account_type
	ExternalOrgID     string // Optional filter - scopes to a single account by provider org id
}

// GetOverviewSummary retrieves aggregated summary metrics for the observability overview.
// When no filters are applied, reads from the pre-aggregated metrics_summaries MV.
// Falls back to scanning telemetry_logs when user_id, external_user_id, or api_key_id filters are set.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetOverviewSummary(ctx context.Context, arg GetOverviewSummaryParams) (*OverviewSummary, error) {
	hasFilters := arg.UserID != "" || arg.ExternalUserID != "" || arg.APIKeyID != "" || arg.ToolsetSlug != "" || arg.RemoteMCPServerID != "" || arg.MCPServerID != "" || arg.EventSource != "" || arg.HookSource != "" || arg.AccountType != "" || arg.ExternalOrgID != ""

	var sb squirrel.SelectBuilder
	if hasFilters {
		sb = q.getOverviewSummaryRaw(arg)
	} else {
		sb = q.getOverviewSummaryMV(arg)
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building overview summary query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return &OverviewSummary{
			TotalChats:               0,
			ResolvedChats:            0,
			FailedChats:              0,
			AvgSessionDurationMs:     0,
			AvgResolutionTimeMs:      0,
			TotalInputTokens:         0,
			TotalOutputTokens:        0,
			TotalTokens:              0,
			CacheReadInputTokens:     0,
			CacheCreationInputTokens: 0,
			TotalCost:                0,
			TotalToolCalls:           0,
			FailedToolCalls:          0,
			AvgLatencyMs:             0,
		}, nil
	}

	var summary OverviewSummary
	if err = rows.ScanStruct(&summary); err != nil {
		return nil, fmt.Errorf("error scanning row: %w", err)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return &summary, nil
}

// getOverviewSummaryMV builds a query against the pre-aggregated metrics_summaries table.
func (q *Queries) getOverviewSummaryMV(arg GetOverviewSummaryParams) squirrel.SelectBuilder {
	return sq.Select(
		"uniqExactIfMerge(total_chats) as total_chats",
		"uniqExactIfMerge(resolved_chats) as resolved_chats",
		"uniqExactIfMerge(failed_chats) as failed_chats",
		"avgIfMerge(avg_chat_duration_ms) as avg_session_duration_ms",
		"avgIfMerge(avg_resolution_time_ms) as avg_resolution_time_ms",
		"sumIfMerge(total_input_tokens) as total_input_tokens",
		"sumIfMerge(total_output_tokens) as total_output_tokens",
		"sumIfMerge(total_tokens) as total_tokens",
		"sumIfMerge(cache_read_input_tokens) as cache_read_input_tokens",
		"sumIfMerge(cache_creation_input_tokens) as cache_creation_input_tokens",
		"sumIfMerge(total_cost) as total_cost",
		"countIfMerge(total_tool_calls) as total_tool_calls",
		"countIfMerge(tool_call_failure) as failed_tool_calls",
		"avgIfMerge(avg_tool_duration_ms) as avg_latency_ms",
	).
		From("metrics_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_bucket >= toStartOfHour(fromUnixTimestamp64Nano(?))", arg.TimeStart).
		Where("time_bucket < toStartOfHour(fromUnixTimestamp64Nano(?))", arg.TimeEnd)
}

// getOverviewSummaryRaw builds a query against the raw telemetry_logs table (used when filters are applied).
func (q *Queries) getOverviewSummaryRaw(arg GetOverviewSummaryParams) squirrel.SelectBuilder {
	sb := sq.Select(
		"uniqExactIf(chat_id, chat_id != '') as total_chats",
		"uniqExactIf(chat_id, chat_id != '' AND evaluation_score_label = 'success') as resolved_chats",
		"uniqExactIf(chat_id, chat_id != '' AND evaluation_score_label = 'failure') as failed_chats",
		"if(isNaN(avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gen_ai.conversation.duration) != '')), 0, avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gen_ai.conversation.duration) != '')) as avg_session_duration_ms",
		"if(isNaN(avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, evaluation_score_label = 'success')), 0, avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, evaluation_score_label = 'success')) as avg_resolution_time_ms",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') as total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') as total_output_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') as total_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '') as cache_read_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '') as cache_creation_input_tokens",
		"sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') as total_cost",
		"countIf(startsWith(gram_urn, 'tools:')) as total_tool_calls",
		"countIf(startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) as failed_tool_calls",
		"if(isNaN(avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(gram_urn, 'tools:'))), 0, avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(gram_urn, 'tools:'))) as avg_latency_ms",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd)

	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{userIdentifierExpr("external_user_id"): arg.ExternalUserID})
	}
	if arg.UserID != "" {
		sb = sb.Where(squirrel.Eq{userIdentifierExpr("user_id"): arg.UserID})
	}
	if arg.APIKeyID != "" {
		sb = sb.Where(squirrel.Eq{"api_key_id": arg.APIKeyID})
	}
	if arg.ToolsetSlug != "" {
		sb = sb.Where(squirrel.Eq{"toolset_slug": arg.ToolsetSlug})
	}
	if arg.RemoteMCPServerID != "" {
		sb = sb.Where(squirrel.Eq{"remote_mcp_server_id": arg.RemoteMCPServerID})
	}
	if arg.MCPServerID != "" {
		sb = sb.Where(squirrel.Eq{"mcp_server_id": arg.MCPServerID})
	}
	if arg.EventSource != "" {
		sb = sb.Where(squirrel.Eq{"event_source": arg.EventSource})
	}
	if arg.HookSource != "" {
		sb = sb.Where(squirrel.Eq{"hook_source": arg.HookSource})
	}
	sb = withAccountTypeFilter(sb, arg.AccountType)
	if arg.ExternalOrgID != "" {
		sb = sb.Where(squirrel.Eq{"external_org_id": arg.ExternalOrgID})
	}

	return sb
}

// ListChatsParams contains the parameters for listing chats.
type ListChatsParams struct {
	GramProjectID    string
	TimeStart        int64
	TimeEnd          int64
	GramDeploymentID string
	GramURN          string
	UserID           string
	ExternalUserID   string
	SortOrder        string
	Cursor           string // gram_chat_id to paginate from
	Limit            int
}

// ListChats retrieves aggregated chat summaries grouped by gram_chat_id.
//
// Original SQL reference:
// SELECT gram_chat_id, min(time_unix_nano), max(time_unix_nano), ... FROM telemetry_logs
// WHERE gram_project_id = ? AND time_unix_nano >= ? AND time_unix_nano <= ?
// [+ optional filters] GROUP BY gram_chat_id ORDER BY start_time_unix_nano LIMIT ?
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListChats(ctx context.Context, arg ListChatsParams) ([]ChatSummary, error) {
	sb := sq.Select(
		"gram_chat_id",
		"min(time_unix_nano) as start_time_unix_nano",
		"max(time_unix_nano) as end_time_unix_nano",
		"count(*) as log_count",
		"countIf(startsWith(gram_urn, 'tools:')) as tool_call_count",
		// Message count: count unique LLM responses by gen_ai.response.id
		"uniqExactIf(toString(attributes.gen_ai.response.id), toString(attributes.gen_ai.response.id) != '') as message_count",
		// Duration in seconds (max event time - min event time)
		"toFloat64(max(time_unix_nano) - min(time_unix_nano)) / 1000000000.0 as duration_seconds",
		// Status: failed if any tool call returned 4xx/5xx, otherwise success
		"if(countIf(startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) > 0, 'error', 'success') as status",
		"anyIf(toString(attributes.user.id), toString(attributes.user.id) != '') as user_id",
		// Model used (pick any non-empty response model from completion events)
		"anyIf(toString(attributes.gen_ai.response.model), toString(attributes.gen_ai.response.model) != '') as model",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') as total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') as total_output_tokens",
		totalTokensExpr+" as total_tokens",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where("gram_chat_id IS NOT NULL").
		Where("gram_chat_id != ''")

	// Optional filters
	if arg.GramDeploymentID != "" {
		sb = sb.Where("gram_deployment_id = toUUIDOrNull(?)", arg.GramDeploymentID)
	}
	if arg.GramURN != "" {
		sb = sb.Where("position(telemetry_logs.gram_urn, ?) > 0", arg.GramURN)
	}
	if arg.UserID != "" {
		sb = sb.Where(squirrel.Eq{"user_id": arg.UserID})
	}
	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}

	sb = sb.GroupBy("gram_chat_id")

	// HAVING clause for cursor pagination with tuple comparison for tie-breaking
	sb = withHavingTuplePagination(sb, arg.Cursor, arg.SortOrder, arg.GramProjectID, "gram_chat_id", "min(time_unix_nano)", "", nil)

	// Ordering - include gram_chat_id as secondary for stable ordering
	sb = withOrdering(sb, arg.SortOrder, "start_time_unix_nano", "gram_chat_id")

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list chats query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []ChatSummary
	for rows.Next() {
		var chat ChatSummary
		if err = rows.ScanStruct(&chat); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		chats = append(chats, chat)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return chats, nil
}

// GetChatMetricsByIDsParams contains the parameters for getting metrics for specific chat IDs.
type GetChatMetricsByIDsParams struct {
	GramProjectID string
	ChatIDs       []string // UUIDs of chats to get metrics for
}

// ChatMetricsRow represents token and cost metrics for a single chat.
type ChatMetricsRow struct {
	GramChatID        string  `ch:"gram_chat_id"`
	TotalInputTokens  int64   `ch:"total_input_tokens"`
	TotalOutputTokens int64   `ch:"total_output_tokens"`
	TotalTokens       int64   `ch:"total_tokens"`
	TotalCost         float64 `ch:"total_cost"`
}

// GetChatMetricsByIDs retrieves token and cost metrics for specific chat IDs.
// This is used to enrich chat overview data from PostgreSQL with metrics from ClickHouse.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetChatMetricsByIDs(ctx context.Context, arg GetChatMetricsByIDsParams) (map[string]ChatMetricsRow, error) {
	if len(arg.ChatIDs) == 0 {
		return make(map[string]ChatMetricsRow), nil
	}

	sb := sq.Select(
		"chat_id as gram_chat_id",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') as total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') as total_output_tokens",
		totalTokensExpr+" as total_tokens",
		"sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') as total_cost",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where(squirrel.Eq{"chat_id": arg.ChatIDs}).
		GroupBy("chat_id")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building get chat metrics by IDs query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	metricsMap := make(map[string]ChatMetricsRow)
	for rows.Next() {
		var metrics ChatMetricsRow
		if err = rows.ScanStruct(&metrics); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		metricsMap[metrics.GramChatID] = metrics
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return metricsMap, nil
}

// GetClaudeTurnUsageByChatIDsParams contains the parameters for getting Claude
// Code per-turn usage for specific chat IDs.
type GetClaudeTurnUsageByChatIDsParams struct {
	GramProjectID string
	ChatIDs       []string
}

// ClaudeTurnUsageRow represents aggregated Claude Code usage for one prompt.id turn.
type ClaudeTurnUsageRow struct {
	GramChatID          string   `ch:"gram_chat_id"`
	PromptID            string   `ch:"prompt_id"`
	StartTimeUnixNano   int64    `ch:"start_time_unix_nano"`
	EndTimeUnixNano     int64    `ch:"end_time_unix_nano"`
	RequestCount        uint64   `ch:"request_count"`
	InputTokens         int64    `ch:"input_tokens"`
	OutputTokens        int64    `ch:"output_tokens"`
	CacheReadTokens     int64    `ch:"cache_read_tokens"`
	CacheCreationTokens int64    `ch:"cache_creation_tokens"`
	TotalTokens         int64    `ch:"total_tokens"`
	CostUSD             float64  `ch:"cost_usd"`
	CostMicros          int64    `ch:"cost_micros"`
	Models              []string `ch:"models"`
	QuerySources        []string `ch:"query_sources"`
}

// ClaudeToolUsageRow represents serialized input/result sizes for one Claude Code tool use.
type ClaudeToolUsageRow struct {
	GramChatID      string `ch:"gram_chat_id"`
	ToolUseID       string `ch:"tool_use_id"`
	PromptID        string `ch:"prompt_id"`
	ToolName        string `ch:"tool_name"`
	InputSizeBytes  int64  `ch:"input_size_bytes"`
	ResultSizeBytes int64  `ch:"result_size_bytes"`
}

// GetClaudeTurnUsageByChatIDs retrieves ordered Claude Code usage turns grouped
// by gram_chat_id and attributes.prompt.id. It initializes each requested chat
// ID to an empty slice so callers can best-effort enrich chat responses without
// special-casing missing ClickHouse rows.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetClaudeTurnUsageByChatIDs(ctx context.Context, arg GetClaudeTurnUsageByChatIDsParams) (map[string][]ClaudeTurnUsageRow, error) {
	usageByChatID := make(map[string][]ClaudeTurnUsageRow, len(arg.ChatIDs))
	for _, chatID := range arg.ChatIDs {
		usageByChatID[chatID] = []ClaudeTurnUsageRow{}
	}
	if len(arg.ChatIDs) == 0 {
		return usageByChatID, nil
	}

	promptIDExpr := "toString(attributes.prompt.id)"
	isAPIRequestExpr := "(toString(attributes.event.name) = 'api_request' OR body = 'claude_code.api_request')"
	isClaudeCodeExpr := "(service_name = 'claude-code' OR toString(resource_attributes.service.name) = 'claude-code' OR startsWith(body, 'claude_code.'))"
	inputTokensExpr := "sumIf(toInt64OrZero(toString(attributes.input_tokens)), " + isAPIRequestExpr + ")"
	outputTokensExpr := "sumIf(toInt64OrZero(toString(attributes.output_tokens)), " + isAPIRequestExpr + ")"
	cacheReadTokensExpr := "sumIf(toInt64OrZero(toString(attributes.cache_read_tokens)), " + isAPIRequestExpr + ")"
	cacheCreationTokensExpr := "sumIf(toInt64OrZero(toString(attributes.cache_creation_tokens)), " + isAPIRequestExpr + ")"

	sb := sq.Select(
		"gram_chat_id",
		promptIDExpr+" AS prompt_id",
		"min(time_unix_nano) AS start_time_unix_nano",
		"max(time_unix_nano) AS end_time_unix_nano",
		"countIf("+isAPIRequestExpr+") AS request_count",
		inputTokensExpr+" AS input_tokens",
		outputTokensExpr+" AS output_tokens",
		cacheReadTokensExpr+" AS cache_read_tokens",
		cacheCreationTokensExpr+" AS cache_creation_tokens",
		"("+inputTokensExpr+" + "+outputTokensExpr+" + "+cacheReadTokensExpr+" + "+cacheCreationTokensExpr+") AS total_tokens",
		"sumIf(toFloat64OrZero(toString(attributes.cost_usd)), "+isAPIRequestExpr+") AS cost_usd",
		"sumIf(toInt64OrZero(toString(attributes.cost_usd_micros)), "+isAPIRequestExpr+") AS cost_micros",
		"arraySort(groupUniqArrayIf(toString(attributes.model), "+isAPIRequestExpr+" AND toString(attributes.model) != '')) AS models",
		"arraySort(groupUniqArrayIf(toString(attributes.query_source), "+isAPIRequestExpr+" AND toString(attributes.query_source) != '')) AS query_sources",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where(squirrel.Eq{"gram_chat_id": arg.ChatIDs}).
		Where("gram_chat_id IS NOT NULL").
		Where("gram_chat_id != ''").
		Where(promptIDExpr+" != ''").
		Where(isClaudeCodeExpr).
		GroupBy("gram_chat_id", promptIDExpr).
		OrderBy("gram_chat_id ASC", "start_time_unix_nano ASC", "prompt_id ASC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building get Claude turn usage by chat IDs query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var usage ClaudeTurnUsageRow
		if err = rows.ScanStruct(&usage); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		usageByChatID[usage.GramChatID] = append(usageByChatID[usage.GramChatID], usage)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return usageByChatID, nil
}

// GetClaudeToolUsageByChatIDs retrieves Claude Code tool input/result byte sizes
// grouped by chat ID and tool_use_id. Claude Code emits these fields on
// tool_result events after the tool completes.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetClaudeToolUsageByChatIDs(ctx context.Context, arg GetClaudeTurnUsageByChatIDsParams) (map[string][]ClaudeToolUsageRow, error) {
	usageByChatID := make(map[string][]ClaudeToolUsageRow, len(arg.ChatIDs))
	for _, chatID := range arg.ChatIDs {
		usageByChatID[chatID] = []ClaudeToolUsageRow{}
	}
	if len(arg.ChatIDs) == 0 {
		return usageByChatID, nil
	}

	toolUseIDExpr := "toString(attributes.tool_use_id)"
	promptIDExpr := "toString(attributes.prompt.id)"
	isToolResultExpr := "(toString(attributes.event.name) = 'tool_result' OR body = 'claude_code.tool_result')"
	isClaudeCodeExpr := "(service_name = 'claude-code' OR toString(resource_attributes.service.name) = 'claude-code' OR startsWith(body, 'claude_code.'))"

	sb := sq.Select(
		"gram_chat_id",
		toolUseIDExpr+" AS tool_use_id",
		promptIDExpr+" AS prompt_id",
		"anyIf(toString(attributes.tool_name), toString(attributes.tool_name) != '') AS tool_name",
		"max(toInt64OrZero(toString(attributes.tool_input_size_bytes))) AS input_size_bytes",
		"max(toInt64OrZero(toString(attributes.tool_result_size_bytes))) AS result_size_bytes",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where(squirrel.Eq{"gram_chat_id": arg.ChatIDs}).
		Where("gram_chat_id IS NOT NULL").
		Where("gram_chat_id != ''").
		Where(toolUseIDExpr+" != ''").
		Where(promptIDExpr+" != ''").
		Where(isToolResultExpr).
		Where(isClaudeCodeExpr).
		GroupBy("gram_chat_id", toolUseIDExpr, promptIDExpr).
		OrderBy("gram_chat_id ASC", "min(time_unix_nano) ASC", "tool_use_id ASC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building get Claude tool usage by chat IDs query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var usage ClaudeToolUsageRow
		if err = rows.ScanStruct(&usage); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		usageByChatID[usage.GramChatID] = append(usageByChatID[usage.GramChatID], usage)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return usageByChatID, nil
}

// toolCallExpressions returns the SQL fragments used to count tool calls for a
// given event source. Hook events (Claude Code, Cursor, Codex) carry a
// tool_name and a hook event name but no gram_urn; we only count the
// completion-side event so each call counts once, not once per pre/post pair.
// Other event sources count Gram MCP tool invocations via gram_urn + HTTP
// status.
type toolCallExpressions struct {
	isCall    string
	isSuccess string
	isFailure string
	key       string
}

func toolCallExprsFor(eventSource string) toolCallExpressions {
	if eventSource == "hook" {
		return toolCallExpressions{
			isCall:    "tool_name != '' AND toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')",
			isSuccess: "tool_name != '' AND toString(attributes.gram.hook.event) = 'PostToolUse'",
			isFailure: "tool_name != '' AND toString(attributes.gram.hook.event) = 'PostToolUseFailure'",
			key:       "tool_name",
		}
	}
	return toolCallExpressions{
		isCall:    "startsWith(gram_urn, 'tools:')",
		isSuccess: "startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300",
		isFailure: "startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400",
		key:       "gram_urn",
	}
}

func chAttr(path string) string {
	return "toString(attributes." + path + ")"
}

func chMultiIf(args ...string) string {
	return "multiIf(" + strings.Join(args, ", ") + ")"
}

func chFirstNonEmpty(exprs ...string) string {
	if len(exprs) == 0 {
		return "''"
	}
	if len(exprs) == 1 {
		return exprs[0]
	}

	args := make([]string, 0, len(exprs)*2-1)
	for _, expr := range exprs[:len(exprs)-1] {
		args = append(args, expr+" != ''", expr)
	}
	args = append(args, exprs[len(exprs)-1])

	return chMultiIf(args...)
}

func chAny(conditions ...string) string {
	wrapped := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		wrapped = append(wrapped, "("+condition+")")
	}

	return "(" + strings.Join(wrapped, " OR ") + ")"
}

// SearchUsersParams contains the parameters for searching users with aggregated metrics.
type SearchUsersParams struct {
	GramProjectID    string
	TimeStart        int64
	TimeEnd          int64
	GramDeploymentID string // optional
	EventSource      string // optional; e.g. "hook"
	HookSource       string // optional; e.g. "cursor"
	AccountType      string // optional; e.g. "personal"
	ExternalOrgID    string // optional; scopes to a single account by provider org id
	GroupBy          string // "user_id" or "external_user_id"
	UserIDs          []string
	SortOrder        string // "asc" or "desc"
	Cursor           string // user identifier to paginate from
	Limit            int
}

// SearchUsers retrieves aggregated usage metrics grouped by user identifier.
//
// Groups telemetry logs by internal email/user_id or external_user_id and
// computes per-user metrics including tokens, chats, and tool call breakdowns.
// Pagination uses last_seen_unix_nano + the group column for stable cursor ordering.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) SearchUsers(ctx context.Context, arg SearchUsersParams) ([]UserSummary, error) {
	groupCol := "user_id"
	if arg.GroupBy == "external_user_id" {
		groupCol = "external_user_id"
	}
	groupExpr := searchUsersGroupExpr(arg.GroupBy)

	tc := toolCallExprsFor(arg.EventSource)

	sb := sq.Select(
		groupExpr+" AS user_id",
		"anyIf(user_email, user_email != '') AS user_email",

		// Activity timestamps
		"min(time_unix_nano) AS first_seen_unix_nano",
		"max(time_unix_nano) AS last_seen_unix_nano",

		// Chat metrics
		"uniqExactIf(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats",
		"uniqExactIf(toString(attributes.gen_ai.response.id), toString(attributes.gen_ai.response.id) != '') AS total_chat_requests",

		// Token metrics (from any event with gen_ai usage data)
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') AS total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') AS total_output_tokens",
		totalTokensExpr+" AS total_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '') AS cache_read_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '') AS cache_creation_input_tokens",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS avg_tokens_per_request",
		"sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') AS total_cost",

		// Tool call metrics (path depends on event source — Gram MCP tools vs AI-coding hook tools)
		"countIf("+tc.isCall+") AS total_tool_calls",
		"countIf("+tc.isSuccess+") AS tool_call_success",
		"countIf("+tc.isFailure+") AS tool_call_failure",

		// Tool breakdowns (maps of tool URN or hook tool name -> count)
		"sumMapIf(map("+tc.key+", toUInt64(1)), "+tc.isCall+") AS tool_counts",
		"sumMapIf(map("+tc.key+", toUInt64(1)), "+tc.isSuccess+") AS tool_success_counts",
		"sumMapIf(map("+tc.key+", toUInt64(1)), "+tc.isFailure+") AS tool_failure_counts",

		// Hook source breakdowns (maps of hook source -> count)
		"sumMapIf(map(hook_source, toUInt64(1)), hook_source != '') AS hook_source_counts",

		// Distinct account types observed (powers the employees personal-account indicator)
		"groupUniqArrayIf(account_type, account_type != '') AS account_types",

		// Raw user_id values folded into this summary. The group key is email-first,
		// so callers joining against user_id-keyed stores (user_accounts, role
		// assignments) need these to find the summary's underlying ids.
		"groupUniqArrayIf(telemetry_logs.user_id, telemetry_logs.user_id != '') AS raw_user_ids",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where(groupExpr + " != ''")

	// Internal grouping keys on email, so rows missing user_email look up the
	// email observed alongside their user_id elsewhere in the window. Without
	// this, a person's email-less rows surface as a separate summary keyed by
	// their raw user_id that carries none of their token usage.
	var joinClause string
	var joinArgs []any
	if arg.GroupBy != "external_user_id" {
		joinClause = searchUsersKnownEmailsJoin
		joinArgs = []any{arg.GramProjectID, arg.TimeStart, arg.TimeEnd}
		sb = sb.LeftJoin(joinClause, joinArgs...)
	}

	// Optional deployment filter
	if arg.GramDeploymentID != "" {
		sb = sb.Where("gram_deployment_id = toUUIDOrNull(?)", arg.GramDeploymentID)
	}
	if arg.EventSource != "" {
		sb = sb.Where("event_source = ?", arg.EventSource)
	}
	if arg.HookSource != "" {
		sb = sb.Where("hook_source = ?", arg.HookSource)
	}
	sb = withAccountTypeFilter(sb, arg.AccountType)
	if arg.ExternalOrgID != "" {
		sb = sb.Where("external_org_id = ?", arg.ExternalOrgID)
	}
	if len(arg.UserIDs) > 0 {
		if arg.GroupBy == "external_user_id" {
			sb = sb.Where(squirrel.Eq{groupExpr: arg.UserIDs})
		} else {
			sb = sb.Where(squirrel.Or{
				squirrel.Eq{groupExpr: arg.UserIDs},
				squirrel.Eq{userIdentifierExpr(groupCol): arg.UserIDs},
			})
		}
	}

	sb = sb.GroupBy(groupExpr)

	// Cursor pagination using last_seen + group column for stable ordering
	sb = withHavingTuplePagination(sb, arg.Cursor, arg.SortOrder, arg.GramProjectID, groupExpr, "max(time_unix_nano)", joinClause, joinArgs)

	// Order by last_seen with group column as tie-breaker
	sb = withOrdering(sb, arg.SortOrder, "last_seen_unix_nano", "user_id")

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building search users query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserSummary
	for rows.Next() {
		var u UserSummary
		if err = rows.ScanStruct(&u); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		users = append(users, u)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// GetUserMetricsSummaryParams contains the parameters for getting a user's metrics summary.
type GetUserMetricsSummaryParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	UserID         string // user_id (mutually exclusive with ExternalUserID)
	ExternalUserID string // external_user_id (mutually exclusive with UserID)
	EventSource    string // Optional filter - filters by event_source
	HookSource     string // Optional filter - filters by hook_source
	AccountType    string // Optional filter - filters by account_type
	ExternalOrgID  string // Optional filter - scopes to a single account by provider org id
}

// GetUserMetricsSummary retrieves aggregated metrics for a specific user.
// Uses the same aggregations as GetMetricsSummary (project metrics) but filtered by user.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetUserMetricsSummary(ctx context.Context, arg GetUserMetricsSummaryParams) (*MetricsSummaryRow, error) {
	tc := toolCallExprsFor(arg.EventSource)

	sb := sq.Select(
		// Activity timestamps
		"min(time_unix_nano) AS first_seen_unix_nano",
		"max(time_unix_nano) AS last_seen_unix_nano",

		// Cardinality (exclude empty strings)
		"uniqExactIf(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats",
		"uniqExactIf(toString(attributes.gen_ai.response.model), toString(attributes.gen_ai.response.model) != '') AS distinct_models",
		"uniqExactIf(toString(attributes.gen_ai.provider.name), toString(attributes.gen_ai.provider.name) != '') AS distinct_providers",

		// Token metrics (from any event with gen_ai usage data)
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') AS total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') AS total_output_tokens",
		totalTokensExpr+" AS total_tokens",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS avg_tokens_per_request",

		// Chat request metrics
		"uniqExactIf(toString(attributes.gen_ai.response.id), toString(attributes.gen_ai.response.id) != '') AS total_chat_requests",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gen_ai.conversation.duration) != '') AS avg_chat_duration_ms",

		// Resolution status
		"countIf(position(toString(attributes.gen_ai.response.finish_reasons), 'stop') > 0) AS finish_reason_stop",
		"countIf(position(toString(attributes.gen_ai.response.finish_reasons), 'tool_calls') > 0) AS finish_reason_tool_calls",

		// Tool call metrics (path depends on event source — Gram MCP tools vs AI-coding hook tools)
		"countIf("+tc.isCall+") AS total_tool_calls",
		"countIf("+tc.isSuccess+") AS tool_call_success",
		"countIf("+tc.isFailure+") AS tool_call_failure",
		"avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(gram_urn, 'tools:')) AS avg_tool_duration_ms",

		// Chat resolution metrics (from AI evaluation of chat outcomes)
		"countIf(evaluation_score_label = 'success') AS chat_resolution_success",
		"countIf(evaluation_score_label = 'failure') AS chat_resolution_failure",
		"countIf(evaluation_score_label = 'partial') AS chat_resolution_partial",
		"countIf(evaluation_score_label = 'abandoned') AS chat_resolution_abandoned",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.evaluation.score.value)), evaluation_score_label != '') AS avg_chat_resolution_score",

		// Model breakdown (map of model name -> count)
		"sumMapIf(map(toString(attributes.gen_ai.response.model), toUInt64(1)), toString(attributes.gen_ai.response.model) != '') AS models",

		// Tool breakdowns (maps of tool URN or hook tool name -> count)
		"sumMapIf(map("+tc.key+", toUInt64(1)), "+tc.isCall+") AS tool_counts",
		"sumMapIf(map("+tc.key+", toUInt64(1)), "+tc.isSuccess+") AS tool_success_counts",
		"sumMapIf(map("+tc.key+", toUInt64(1)), "+tc.isFailure+") AS tool_failure_counts",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd)

	// Filter by user ID (one of these must be set)
	if arg.UserID != "" {
		sb = sb.Where(squirrel.Eq{userIdentifierExpr("user_id"): arg.UserID})
	} else if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{userIdentifierExpr("external_user_id"): arg.ExternalUserID})
	}
	if arg.EventSource != "" {
		sb = sb.Where(squirrel.Eq{"event_source": arg.EventSource})
	}
	if arg.HookSource != "" {
		sb = sb.Where(squirrel.Eq{"hook_source": arg.HookSource})
	}
	sb = withAccountTypeFilter(sb, arg.AccountType)
	if arg.ExternalOrgID != "" {
		sb = sb.Where(squirrel.Eq{"external_org_id": arg.ExternalOrgID})
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building get user metrics summary query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		// Return empty metrics if no rows
		return &MetricsSummaryRow{
			FirstSeenUnixNano:        0,
			LastSeenUnixNano:         0,
			TotalChats:               0,
			DistinctModels:           0,
			DistinctProviders:        0,
			TotalInputTokens:         0,
			TotalOutputTokens:        0,
			TotalTokens:              0,
			CacheReadInputTokens:     0,
			CacheCreationInputTokens: 0,
			AvgTokensPerReq:          0,
			TotalCost:                0,
			TotalChatRequests:        0,
			AvgChatDurationMs:        0,
			FinishReasonStop:         0,
			FinishReasonToolCalls:    0,
			TotalToolCalls:           0,
			ToolCallSuccess:          0,
			ToolCallFailure:          0,
			AvgToolDurationMs:        0,
			ChatResolutionSuccess:    0,
			ChatResolutionFailure:    0,
			ChatResolutionPartial:    0,
			ChatResolutionAbandoned:  0,
			AvgChatResolutionScore:   0,
			Models:                   make(map[string]uint64),
			ToolCounts:               make(map[string]uint64),
			ToolSuccessCounts:        make(map[string]uint64),
			ToolFailureCounts:        make(map[string]uint64),
		}, nil
	}

	var metrics MetricsSummaryRow
	if err = rows.Scan(
		&metrics.FirstSeenUnixNano,
		&metrics.LastSeenUnixNano,
		&metrics.TotalChats,
		&metrics.DistinctModels,
		&metrics.DistinctProviders,
		&metrics.TotalInputTokens,
		&metrics.TotalOutputTokens,
		&metrics.TotalTokens,
		&metrics.AvgTokensPerReq,
		&metrics.TotalChatRequests,
		&metrics.AvgChatDurationMs,
		&metrics.FinishReasonStop,
		&metrics.FinishReasonToolCalls,
		&metrics.TotalToolCalls,
		&metrics.ToolCallSuccess,
		&metrics.ToolCallFailure,
		&metrics.AvgToolDurationMs,
		&metrics.ChatResolutionSuccess,
		&metrics.ChatResolutionFailure,
		&metrics.ChatResolutionPartial,
		&metrics.ChatResolutionAbandoned,
		&metrics.AvgChatResolutionScore,
		&metrics.Models,
		&metrics.ToolCounts,
		&metrics.ToolSuccessCounts,
		&metrics.ToolFailureCounts,
	); err != nil {
		return nil, fmt.Errorf("error scanning row: %w", err)
	}

	return &metrics, nil
}

// GetEmployeeDataFlowGraphParams contains parameters for getting an employee data flow graph.
type GetEmployeeDataFlowGraphParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	UserID         string // user_id (mutually exclusive with ExternalUserID)
	ExternalUserID string // external_user_id (mutually exclusive with UserID)
	AccountType    string // Optional filter - filters by account_type
	ExternalOrgID  string // Optional filter - scopes to a single account by provider org id
}

const employeeDataFlowMaxPathTuples uint64 = 100

type employeeDataFlowExpressions struct {
	origin      string
	client      string
	server      string
	serverClass string
	tool        string
	isCall      string
	isSuccess   string
	isFailure   string
}

func employeeDataFlowExprs() employeeDataFlowExpressions {
	externalMCPName := chFirstNonEmpty(
		chAttr("gram.external_mcp.name"),
		chAttr("gram.external_mcp.slug"),
		chAttr("gram.external_mcp.id"),
		"''",
	)
	mcpMatch := chAttr("gram.mcp.match")
	mcpServerURL := chAttr("gram.mcp.server_url")
	hookEvent := chAttr("gram.hook.event")
	httpStatus := "toInt32OrZero(" + chAttr("http.response.status_code") + ")"
	urnSource := "arrayElement(splitByChar(':', gram_urn), 3)"

	hookCall := "event_source = 'hook' AND tool_name != '' AND " + hookEvent + " IN ('PostToolUse', 'PostToolUseFailure')"
	gramCall := "event_source != 'hook' AND startsWith(gram_urn, 'tools:')"

	server := chMultiIf(
		"remote_mcp_server_id != ''",
		chFirstNonEmpty("tool_source", externalMCPName, "remote_mcp_server_id"),
		externalMCPName+" != ''",
		externalMCPName,
		mcpMatch+" != ''",
		mcpMatch,
		"tool_source != ''",
		"tool_source",
		"startsWith(gram_urn, 'tools:')",
		urnSource,
		"'local'",
	)

	return employeeDataFlowExpressions{
		origin: chAttr("gram.hook.hostname"),
		client: chFirstNonEmpty(
			"hook_source",
			"event_source",
			"'unknown'",
		),
		server: server,
		serverClass: chMultiIf(
			"remote_mcp_server_id != '' OR (startsWith(gram_urn, 'tools:') AND event_source != 'hook')",
			"'gram'",
			externalMCPName+" != '' OR "+mcpServerURL+" != '' OR "+mcpMatch+" != ''",
			"'external'",
			server+" = 'local'",
			"'local'",
			"tool_source = ''",
			"'local'",
			"'external'",
		),
		tool:      chFirstNonEmpty("tool_name", "gram_urn", "urn", "''"),
		isCall:    chAny(hookCall, gramCall),
		isSuccess: chAny("event_source = 'hook' AND tool_name != '' AND "+hookEvent+" = 'PostToolUse'", gramCall+" AND "+httpStatus+" >= 200 AND "+httpStatus+" < 300"),
		isFailure: chAny("event_source = 'hook' AND tool_name != '' AND "+hookEvent+" = 'PostToolUseFailure'", gramCall+" AND "+httpStatus+" >= 400"),
	}
}

// GetEmployeeDataFlowGraph aggregates an employee's tool-call telemetry into
// path tuples: origin -> MCP client -> MCP server -> tool.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetEmployeeDataFlowGraph(ctx context.Context, arg GetEmployeeDataFlowGraphParams) ([]EmployeeDataFlowRow, error) {
	exprs := employeeDataFlowExprs()

	sb := sq.Select(
		exprs.origin+" AS origin",
		exprs.client+" AS client",
		exprs.server+" AS server",
		exprs.serverClass+" AS server_class",
		exprs.tool+" AS tool",
		"count() AS call_count",
		"countIf("+exprs.isSuccess+") AS success_count",
		"countIf("+exprs.isFailure+") AS failure_count",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where(exprs.isCall)

	if arg.UserID != "" {
		sb = sb.Where(squirrel.Eq{userIdentifierExpr("user_id"): arg.UserID})
	} else if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{userIdentifierExpr("external_user_id"): arg.ExternalUserID})
	}
	sb = withAccountTypeFilter(sb, arg.AccountType)
	if arg.ExternalOrgID != "" {
		sb = sb.Where(squirrel.Eq{"external_org_id": arg.ExternalOrgID})
	}

	sb = sb.GroupBy("origin", "client", "server", "server_class", "tool").
		OrderBy("call_count DESC").
		Limit(employeeDataFlowMaxPathTuples)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building get employee data flow graph query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	graphRows := []EmployeeDataFlowRow{}
	for rows.Next() {
		var row EmployeeDataFlowRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		graphRows = append(graphRows, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return graphRows, nil
}

// ListFilterOptionsParams contains the parameters for listing filter options.
type ListFilterOptionsParams struct {
	GramProjectID string
	TimeStart     int64
	TimeEnd       int64
	FilterType    string // "api_key", "user", "internal_user", or "agent"
	EventSource   string // Optional filter - filters by event_source
	Limit         int
}

// ListFilterOptions retrieves distinct filter values for a time period.
// Results are sorted by event count descending.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListFilterOptions(ctx context.Context, arg ListFilterOptionsParams) ([]FilterOption, error) {
	var groupCol string
	switch arg.FilterType {
	case "api_key":
		groupCol = "api_key_id"
	case "user":
		groupCol = "external_user_id"
	case "internal_user":
		groupCol = "user_id"
	case "agent":
		groupCol = "hook_source"
	default:
		return nil, fmt.Errorf("invalid filter type: %s", arg.FilterType)
	}

	sb := sq.Select(
		groupCol+" AS id",
		groupCol+" AS label",               // For now, label is same as ID
		"uniqExact(gram_chat_id) AS count", // Count unique chat sessions, not log rows
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where(groupCol + " != ''").
		GroupBy(groupCol).
		OrderBy("count DESC").
		Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	if arg.EventSource != "" {
		sb = sb.Where(squirrel.Eq{"event_source": arg.EventSource})
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list filter options query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var options []FilterOption
	for rows.Next() {
		var opt FilterOption
		if err = rows.ScanStruct(&opt); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		options = append(options, opt)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return options, nil
}

// ListAttributeKeysParams defines the parameters for listing distinct attribute keys.
type ListAttributeKeysParams struct {
	GramProjectID string
	TimeStart     int64
	TimeEnd       int64
}

// ListAttributeKeys retrieves distinct attribute paths from the attribute_keys materialized view for a project and time range.
// Raw paths are returned as-is; the caller is responsible for any display transformation.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListAttributeKeys(ctx context.Context, arg ListAttributeKeysParams) ([]string, error) {
	sb := sq.Select("attribute_key").
		From("attribute_keys").
		Where("gram_project_id = ?", arg.GramProjectID).
		GroupBy("attribute_key").
		Having("max(last_seen_unix_nano) >= ?", arg.TimeStart).
		Having("min(first_seen_unix_nano) <= ?", arg.TimeEnd).
		OrderBy("attribute_key")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list attribute keys query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scanning attribute key: %w", err)
		}
		keys = append(keys, key)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return keys, nil
}

// HooksServerSummaryRow contains aggregated hooks metrics for a single server.
type HooksServerSummaryRow struct {
	ServerName   string  `ch:"server_name"`
	EventCount   uint64  `ch:"event_count"`
	UniqueTools  uint64  `ch:"unique_tools"`
	SuccessCount uint64  `ch:"success_count"`
	FailureCount uint64  `ch:"failure_count"`
	FailureRate  float64 `ch:"failure_rate"`
}

const (
	ToolUsageTargetTypeHostedMCP   = "hosted_mcp_server"
	ToolUsageTargetTypeTunneledMCP = "tunneled_mcp_server"
	ToolUsageTargetTypeShadowMCP   = "shadow_mcp_server"
	ToolUsageTargetTypeLocalTool   = "local_tool"
	ToolUsageTargetTypeSkill       = "skill"

	toolUsageTargetKindServer     = "server"
	toolUsageTargetKindLocalTools = "local_tools"
	toolUsageTargetKindSkill      = "skill"

	toolUsageUserKindEmail          = "email"
	toolUsageUserKindExternalUserID = "external_user_id"
	toolUsageUserKindUserID         = "user_id"
	toolUsageUserKindUnknown        = "unknown"
)

// ToolUsageUserFilter filters unified tool usage by typed user identity.
type ToolUsageUserFilter struct {
	Kind string
	Key  string
}

// HostedMCPMatcher maps hook-observed hosted MCP identifiers to a hosted toolset.
type HostedMCPMatcher struct {
	ToolsetSlug string
	ToolsetName string
	McpSlug     string
}

// MCPServerMatcher maps direct MCP server source ids from telemetry to their
// fronting mcp_servers target. Remote-backed servers stay hosted MCP; tunneled
// servers receive their own target type.
type MCPServerMatcher struct {
	SourceID    string
	TargetType  string
	TargetID    string
	TargetLabel string
}

// GetToolUsageSummaryParams defines the parameters for target-aware tool usage.
type GetToolUsageSummaryParams struct {
	GramProjectID      string
	TimeStart          int64
	TimeEnd            int64
	BucketSizeNs       int64
	HostedMCPMatchers  []HostedMCPMatcher
	MCPServerMatchers  []MCPServerMatcher
	TargetTypes        []string
	HostedToolsetSlugs []string
	ShadowServerNames  []string
	UserFilters        []ToolUsageUserFilter
	HookSources        []string
	AccountType        string // Optional filter - filters by account_type (team|personal)
	TargetLimit        uint64
	UserLimit          uint64
	UsersByTargetLimit uint64
	TargetToolRowLimit uint64
	TimeSeriesRowLimit uint64
	UserSeriesRowLimit uint64
}

type ListToolUsageTracesParams struct {
	GramProjectID      string
	TimeStart          int64
	TimeEnd            int64
	HostedMCPMatchers  []HostedMCPMatcher
	MCPServerMatchers  []MCPServerMatcher
	TargetTypes        []string
	HostedToolsetSlugs []string
	ShadowServerNames  []string
	UserFilters        []ToolUsageUserFilter
	HookSources        []string
	AccountType        string   // Optional filter - personal = exactly personal; team = not personal (includes unclassified)
	Statuses           []string // Optional trace-outcome filter: error, success, blocked, pending. Empty means all.
	Query              string
	Filters            []AttributeFilter
	SortOrder          string
	CursorTimeUnixNano int64
	CursorID           string
	Limit              int
}

// ToolUsageSummary contains bounded chart-ready tool usage aggregates.
type ToolUsageSummary struct {
	Totals              ToolUsageTotalsRow
	Targets             []ToolUsageTargetSummaryRow
	Users               []ToolUsageUserSummaryRow
	TargetTimeSeries    []ToolUsageTargetTimeSeriesPointRow
	UserTimeSeries      []ToolUsageUserTimeSeriesPointRow
	UsersByTarget       []ToolUsageUsersByTargetRow
	TargetToolBreakdown []ToolUsageTargetToolBreakdownRow
}

type ToolUsageTraceSummary struct {
	ID                string  `ch:"id"`
	TraceID           string  `ch:"trace_id"`
	LogGroupKind      string  `ch:"log_group_kind"`
	LogGroupValue     string  `ch:"log_group_value"`
	StartTimeUnixNano int64   `ch:"start_time_unix_nano"`
	LogCount          uint64  `ch:"log_count"`
	GramURN           string  `ch:"gram_urn"`
	ToolName          string  `ch:"tool_name"`
	TargetType        string  `ch:"target_type"`
	TargetKind        string  `ch:"target_kind"`
	TargetID          string  `ch:"target_id"`
	TargetLabel       string  `ch:"target_label"`
	UserKey           string  `ch:"user_key"`
	UserLabel         string  `ch:"user_label"`
	UserKind          string  `ch:"user_kind"`
	HookSource        *string `ch:"hook_source"`
	EventSource       string  `ch:"event_source"`
	HTTPStatusCode    *int32  `ch:"http_status_code"`
	HookStatus        *string `ch:"hook_status"`
	BlockReason       *string `ch:"block_reason"`
	AccountType       *string `ch:"account_type"`
}

// GetToolUsageFilterOptionsParams defines the parameters for tool usage filter option queries.
type GetToolUsageFilterOptionsParams struct {
	GramProjectID     string
	TimeStart         int64
	TimeEnd           int64
	HostedMCPMatchers []HostedMCPMatcher
	MCPServerMatchers []MCPServerMatcher
}

// ToolUsageFilterOptions contains all selectable usage-derived filter options for a time window.
type ToolUsageFilterOptions struct {
	HostedServers []ToolUsageHostedServerFilterOptionRow
	ShadowServers []ToolUsageShadowServerFilterOptionRow
	Users         []ToolUsageUserFilterOptionRow
}

type ToolUsageHostedServerFilterOptionRow struct {
	ToolsetSlug string `ch:"toolset_slug"`
	EventCount  uint64 `ch:"event_count"`
}

type ToolUsageShadowServerFilterOptionRow struct {
	ServerName string `ch:"server_name"`
	EventCount uint64 `ch:"event_count"`
}

type ToolUsageUserFilterOptionRow struct {
	UserKey    string `ch:"user_key"`
	UserLabel  string `ch:"user_label"`
	UserKind   string `ch:"user_kind"`
	EventCount uint64 `ch:"event_count"`
}

type ToolUsageTotalsRow struct {
	EventCount    uint64  `ch:"event_count"`
	SuccessCount  uint64  `ch:"success_count"`
	FailureCount  uint64  `ch:"failure_count"`
	FailureRate   float64 `ch:"failure_rate"`
	UniqueTools   uint64  `ch:"unique_tools"`
	UniqueUsers   uint64  `ch:"unique_users"`
	UniqueTargets uint64  `ch:"unique_targets"`
}

type ToolUsageTargetSummaryRow struct {
	TargetType   string  `ch:"target_type"`
	TargetKind   string  `ch:"target_kind"`
	TargetID     string  `ch:"target_id"`
	TargetLabel  string  `ch:"target_label"`
	EventCount   uint64  `ch:"event_count"`
	UniqueTools  uint64  `ch:"unique_tools"`
	SuccessCount uint64  `ch:"success_count"`
	FailureCount uint64  `ch:"failure_count"`
	FailureRate  float64 `ch:"failure_rate"`
}

type ToolUsageUserSummaryRow struct {
	UserKey      string  `ch:"user_key"`
	UserLabel    string  `ch:"user_label"`
	UserKind     string  `ch:"user_kind"`
	EventCount   uint64  `ch:"event_count"`
	UniqueTools  uint64  `ch:"unique_tools"`
	SuccessCount uint64  `ch:"success_count"`
	FailureCount uint64  `ch:"failure_count"`
	FailureRate  float64 `ch:"failure_rate"`
}

type ToolUsageTargetTimeSeriesPointRow struct {
	BucketStartNs int64  `ch:"bucket_start_ns"`
	TargetType    string `ch:"target_type"`
	TargetKind    string `ch:"target_kind"`
	TargetID      string `ch:"target_id"`
	TargetLabel   string `ch:"target_label"`
	EventCount    uint64 `ch:"event_count"`
	FailureCount  uint64 `ch:"failure_count"`
}

type ToolUsageUserTimeSeriesPointRow struct {
	BucketStartNs int64  `ch:"bucket_start_ns"`
	UserKey       string `ch:"user_key"`
	UserLabel     string `ch:"user_label"`
	UserKind      string `ch:"user_kind"`
	EventCount    uint64 `ch:"event_count"`
	FailureCount  uint64 `ch:"failure_count"`
}

type ToolUsageUsersByTargetRow struct {
	TargetType   string `ch:"target_type"`
	TargetKind   string `ch:"target_kind"`
	TargetID     string `ch:"target_id"`
	TargetLabel  string `ch:"target_label"`
	UserKey      string `ch:"user_key"`
	UserLabel    string `ch:"user_label"`
	UserKind     string `ch:"user_kind"`
	EventCount   uint64 `ch:"event_count"`
	FailureCount uint64 `ch:"failure_count"`
}

type ToolUsageTargetToolBreakdownRow struct {
	TargetType   string  `ch:"target_type"`
	TargetKind   string  `ch:"target_kind"`
	TargetID     string  `ch:"target_id"`
	TargetLabel  string  `ch:"target_label"`
	ToolName     string  `ch:"tool_name"`
	EventCount   uint64  `ch:"event_count"`
	SuccessCount uint64  `ch:"success_count"`
	FailureCount uint64  `ch:"failure_count"`
	FailureRate  float64 `ch:"failure_rate"`
}

// GetToolUsageSummary retrieves target-aware MCP and tool usage aggregates.
func (q *Queries) GetToolUsageSummary(ctx context.Context, arg GetToolUsageSummaryParams) (*ToolUsageSummary, error) {
	totals, err := q.getToolUsageTotals(ctx, arg)
	if err != nil {
		return nil, err
	}

	targets, err := q.getToolUsageTargets(ctx, arg)
	if err != nil {
		return nil, err
	}

	users, err := q.getToolUsageUsers(ctx, arg)
	if err != nil {
		return nil, err
	}

	targetTimeSeries, err := q.getToolUsageTargetTimeSeries(ctx, arg)
	if err != nil {
		return nil, err
	}

	userTimeSeries, err := q.getToolUsageUserTimeSeries(ctx, arg)
	if err != nil {
		return nil, err
	}

	usersByTarget, err := q.getToolUsageUsersByTarget(ctx, arg)
	if err != nil {
		return nil, err
	}

	targetToolBreakdown, err := q.getToolUsageTargetToolBreakdown(ctx, arg)
	if err != nil {
		return nil, err
	}

	return &ToolUsageSummary{
		Totals:              totals,
		Targets:             targets,
		Users:               users,
		TargetTimeSeries:    targetTimeSeries,
		UserTimeSeries:      userTimeSeries,
		UsersByTarget:       usersByTarget,
		TargetToolBreakdown: targetToolBreakdown,
	}, nil
}

// GetToolUsageFilterOptions retrieves usage-derived tool usage filter options for a time window.
func (q *Queries) GetToolUsageFilterOptions(ctx context.Context, arg GetToolUsageFilterOptionsParams) (*ToolUsageFilterOptions, error) {
	summaryArg := GetToolUsageSummaryParams{
		GramProjectID:      arg.GramProjectID,
		TimeStart:          arg.TimeStart,
		TimeEnd:            arg.TimeEnd,
		BucketSizeNs:       0,
		HostedMCPMatchers:  arg.HostedMCPMatchers,
		MCPServerMatchers:  arg.MCPServerMatchers,
		TargetTypes:        nil,
		HostedToolsetSlugs: nil,
		ShadowServerNames:  nil,
		UserFilters:        nil,
		HookSources:        nil,
		AccountType:        "", // filter options enumerate all values, never scoped
		TargetLimit:        0,
		UserLimit:          0,
		UsersByTargetLimit: 0,
		TargetToolRowLimit: 0,
		TimeSeriesRowLimit: 0,
		UserSeriesRowLimit: 0,
	}

	hostedServers, err := q.getToolUsageHostedServerFilterOptions(ctx, summaryArg)
	if err != nil {
		return nil, err
	}

	shadowServers, err := q.getToolUsageShadowServerFilterOptions(ctx, summaryArg)
	if err != nil {
		return nil, err
	}

	users, err := q.getToolUsageUserFilterOptions(ctx, summaryArg)
	if err != nil {
		return nil, err
	}

	return &ToolUsageFilterOptions{
		HostedServers: hostedServers,
		ShadowServers: shadowServers,
		Users:         users,
	}, nil
}

// ListToolUsageTraces retrieves target-aware trace rows for the unified Tool Logs page.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListToolUsageTraces(ctx context.Context, arg ListToolUsageTracesParams) ([]ToolUsageTraceSummary, error) {
	cteSQL, cteArgs, err := toolUsageTraceRowsCTE(arg)
	if err != nil {
		return nil, err
	}

	sb := sq.Select(
		"id",
		"trace_id",
		"log_group_kind",
		"log_group_value",
		"start_time_unix_nano",
		"log_count",
		"gram_urn",
		"tool_name",
		"target_type",
		"target_kind",
		"target_id",
		"target_label",
		"user_key",
		"user_label",
		"user_kind",
		"hook_source",
		"event_source",
		"http_status_code",
		"hook_status",
		"block_reason",
		"account_type",
	).From("normalized_traces")

	if len(arg.TargetTypes) > 0 {
		sb = sb.Where(squirrel.Eq{"target_type": arg.TargetTypes})
	}

	if len(arg.HostedToolsetSlugs) > 0 || len(arg.ShadowServerNames) > 0 {
		targetFilters := squirrel.Or{}
		if len(arg.HostedToolsetSlugs) > 0 {
			targetFilters = append(targetFilters, squirrel.And{
				squirrel.Eq{"target_type": ToolUsageTargetTypeHostedMCP},
				squirrel.Eq{"target_id": arg.HostedToolsetSlugs},
			})
		}
		if len(arg.ShadowServerNames) > 0 {
			targetFilters = append(targetFilters, squirrel.And{
				squirrel.Eq{"target_type": ToolUsageTargetTypeShadowMCP},
				squirrel.Eq{"target_id": arg.ShadowServerNames},
			})
		}
		sb = sb.Where(targetFilters)
	}

	if len(arg.HookSources) > 0 {
		sb = sb.Where(squirrel.Eq{"hook_source": arg.HookSources})
	}

	// account_type is carried on trace_summaries (fast path) and materialized on
	// telemetry_logs (raw path), so this filters on the projected column either
	// way.
	sb = withAccountTypeFilter(sb, arg.AccountType)

	if len(arg.UserFilters) > 0 {
		userFilters := squirrel.Or{}
		for _, filter := range arg.UserFilters {
			userFilters = append(userFilters, squirrel.And{
				squirrel.Eq{"user_kind": filter.Kind},
				squirrel.Eq{"user_key": filter.Key},
			})
		}
		sb = sb.Where(userFilters)
	}

	// http.response.status_code filters are applied here, against the aggregated
	// per-trace http_status_code column, instead of being pushed down to raw rows in
	// toolUsageTraceRowsCTE. See toolUsageHTTPStatusPath for why.
	for _, filter := range arg.Filters {
		if filter.Path != toolUsageHTTPStatusPath || !validJSONPath.MatchString(filter.Path) {
			continue
		}
		if pred := toolUsageStatusPredicate(filter); pred != nil {
			sb = sb.Where(pred)
		}
	}

	// First-class Status filter, applied to the aggregated per-trace outcome columns.
	if pred := toolUsageOutcomePredicate(arg.Statuses); pred != nil {
		sb = sb.Where(pred)
	}

	if arg.CursorID != "" {
		if arg.SortOrder == "asc" {
			sb = sb.Where("(start_time_unix_nano, id) > (?, ?)", arg.CursorTimeUnixNano, arg.CursorID)
		} else {
			sb = sb.Where("(start_time_unix_nano, id) < (?, ?)", arg.CursorTimeUnixNano, arg.CursorID)
		}
	}

	if arg.SortOrder == "asc" {
		sb = sb.OrderBy("start_time_unix_nano ASC", "id ASC")
	} else {
		sb = sb.OrderBy("start_time_unix_nano DESC", "id DESC")
	}

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // validated by service layer

	query, queryArgs, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool usage traces query: %w", err)
	}
	query = cteSQL + " " + query
	queryArgs = append(cteArgs, queryArgs...)

	rows, err := q.conn.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ToolUsageTraceSummary{}
	for rows.Next() {
		var row ToolUsageTraceSummary
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan tool usage trace row: %w", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) getToolUsageTotals(ctx context.Context, arg GetToolUsageSummaryParams) (ToolUsageTotalsRow, error) {
	sb, err := toolUsageFilteredSelect(arg,
		"count() AS event_count",
		"sum(success) AS success_count",
		"sum(failure) AS failure_count",
		"failure_count / greatest(success_count + failure_count, 1) AS failure_rate",
		"uniqExact(tool_name) AS unique_tools",
		"uniqExact(user_kind || ':' || user_key) AS unique_users",
		"uniqExact(target_type || ':' || target_kind || ':' || target_id) AS unique_targets",
	)
	if err != nil {
		return ToolUsageTotalsRow{}, fmt.Errorf("building tool usage totals source: %w", err)
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return ToolUsageTotalsRow{}, fmt.Errorf("building tool usage totals query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return ToolUsageTotalsRow{}, err
	}
	defer rows.Close()

	if !rows.Next() {
		return ToolUsageTotalsRow{
			EventCount:    0,
			SuccessCount:  0,
			FailureCount:  0,
			FailureRate:   0,
			UniqueTools:   0,
			UniqueUsers:   0,
			UniqueTargets: 0,
		}, nil
	}

	var totals ToolUsageTotalsRow
	if err = rows.ScanStruct(&totals); err != nil {
		return ToolUsageTotalsRow{}, fmt.Errorf("scan tool usage totals row: %w", err)
	}
	if err = rows.Err(); err != nil {
		return ToolUsageTotalsRow{}, err
	}

	return totals, nil
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) getToolUsageTargets(ctx context.Context, arg GetToolUsageSummaryParams) ([]ToolUsageTargetSummaryRow, error) {
	sb, err := toolUsageFilteredSelect(arg,
		"target_type",
		"target_kind",
		"target_id",
		"target_label",
		"count() AS event_count",
		"uniqExact(tool_name) AS unique_tools",
		"sum(success) AS success_count",
		"sum(failure) AS failure_count",
		"failure_count / greatest(success_count + failure_count, 1) AS failure_rate",
	)
	if err != nil {
		return nil, fmt.Errorf("building tool usage targets source: %w", err)
	}
	sb = sb.
		GroupBy("target_type", "target_kind", "target_id", "target_label").
		OrderBy("event_count DESC", "target_label ASC").
		Limit(nonZeroLimit(arg.TargetLimit, 25))

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool usage targets query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ToolUsageTargetSummaryRow{}
	for rows.Next() {
		var row ToolUsageTargetSummaryRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan tool usage target row: %w", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) getToolUsageUsers(ctx context.Context, arg GetToolUsageSummaryParams) ([]ToolUsageUserSummaryRow, error) {
	sb, err := toolUsageFilteredSelect(arg,
		"user_key",
		"user_label",
		"user_kind",
		"count() AS event_count",
		"uniqExact(tool_name) AS unique_tools",
		"sum(success) AS success_count",
		"sum(failure) AS failure_count",
		"failure_count / greatest(success_count + failure_count, 1) AS failure_rate",
	)
	if err != nil {
		return nil, fmt.Errorf("building tool usage users source: %w", err)
	}
	sb = sb.
		GroupBy("user_key", "user_label", "user_kind").
		OrderBy("event_count DESC", "user_label ASC").
		Limit(nonZeroLimit(arg.UserLimit, 25))

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool usage users query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ToolUsageUserSummaryRow{}
	for rows.Next() {
		var row ToolUsageUserSummaryRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan tool usage user row: %w", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) getToolUsageTargetTimeSeries(ctx context.Context, arg GetToolUsageSummaryParams) ([]ToolUsageTargetTimeSeriesPointRow, error) {
	bucketExpr := fmt.Sprintf("intDiv(event_time_ns, %d) * %d AS bucket_start_ns", arg.BucketSizeNs, arg.BucketSizeNs)
	sb, err := toolUsageFilteredSelect(arg,
		bucketExpr,
		"target_type",
		"target_kind",
		"target_id",
		"target_label",
		"count() AS event_count",
		"sum(failure) AS failure_count",
	)
	if err != nil {
		return nil, fmt.Errorf("building tool usage target time series source: %w", err)
	}
	sb = sb.
		GroupBy("bucket_start_ns", "target_type", "target_kind", "target_id", "target_label").
		OrderBy("bucket_start_ns ASC", "event_count DESC").
		Limit(nonZeroLimit(arg.TimeSeriesRowLimit, 10000))

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool usage target time series query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ToolUsageTargetTimeSeriesPointRow{}
	for rows.Next() {
		var row ToolUsageTargetTimeSeriesPointRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan tool usage target time series row: %w", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) getToolUsageUserTimeSeries(ctx context.Context, arg GetToolUsageSummaryParams) ([]ToolUsageUserTimeSeriesPointRow, error) {
	bucketExpr := fmt.Sprintf("intDiv(event_time_ns, %d) * %d AS bucket_start_ns", arg.BucketSizeNs, arg.BucketSizeNs)
	sb, err := toolUsageFilteredSelect(arg,
		bucketExpr,
		"user_key",
		"user_label",
		"user_kind",
		"count() AS event_count",
		"sum(failure) AS failure_count",
	)
	if err != nil {
		return nil, fmt.Errorf("building tool usage user time series source: %w", err)
	}
	sb = sb.
		GroupBy("bucket_start_ns", "user_key", "user_label", "user_kind").
		OrderBy("bucket_start_ns ASC", "event_count DESC").
		Limit(nonZeroLimit(arg.UserSeriesRowLimit, 10000))

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool usage user time series query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ToolUsageUserTimeSeriesPointRow{}
	for rows.Next() {
		var row ToolUsageUserTimeSeriesPointRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan tool usage user time series row: %w", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) getToolUsageUsersByTarget(ctx context.Context, arg GetToolUsageSummaryParams) ([]ToolUsageUsersByTargetRow, error) {
	sb, err := toolUsageFilteredSelect(arg,
		"target_type",
		"target_kind",
		"target_id",
		"target_label",
		"user_key",
		"user_label",
		"user_kind",
		"count() AS event_count",
		"sum(failure) AS failure_count",
	)
	if err != nil {
		return nil, fmt.Errorf("building tool usage users by target source: %w", err)
	}
	sb = sb.
		GroupBy("target_type", "target_kind", "target_id", "target_label", "user_key", "user_label", "user_kind").
		OrderBy("event_count DESC", "target_label ASC", "user_label ASC").
		Limit(nonZeroLimit(arg.UsersByTargetLimit, 100))

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool usage users by target query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ToolUsageUsersByTargetRow{}
	for rows.Next() {
		var row ToolUsageUsersByTargetRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan tool usage users by target row: %w", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) getToolUsageTargetToolBreakdown(ctx context.Context, arg GetToolUsageSummaryParams) ([]ToolUsageTargetToolBreakdownRow, error) {
	sb, err := toolUsageFilteredSelect(arg,
		"target_type",
		"target_kind",
		"target_id",
		"target_label",
		"tool_name",
		"count() AS event_count",
		"sum(success) AS success_count",
		"sum(failure) AS failure_count",
		"failure_count / greatest(success_count + failure_count, 1) AS failure_rate",
	)
	if err != nil {
		return nil, fmt.Errorf("building tool usage target tool breakdown source: %w", err)
	}
	sb = sb.
		GroupBy("target_type", "target_kind", "target_id", "target_label", "tool_name").
		OrderBy("event_count DESC", "target_label ASC", "tool_name ASC").
		Limit(nonZeroLimit(arg.TargetToolRowLimit, 100))

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool usage target tool breakdown query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ToolUsageTargetToolBreakdownRow{}
	for rows.Next() {
		var row ToolUsageTargetToolBreakdownRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan tool usage target tool row: %w", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) getToolUsageHostedServerFilterOptions(ctx context.Context, arg GetToolUsageSummaryParams) ([]ToolUsageHostedServerFilterOptionRow, error) {
	sb, err := toolUsageBaseSelect(arg,
		"target_id AS toolset_slug",
		"count() AS event_count",
	)
	if err != nil {
		return nil, fmt.Errorf("building tool usage hosted server filter options source: %w", err)
	}
	sb = sb.
		Where(squirrel.Eq{"target_type": ToolUsageTargetTypeHostedMCP}).
		GroupBy("toolset_slug").
		OrderBy("event_count DESC", "toolset_slug ASC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool usage hosted server filter options query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ToolUsageHostedServerFilterOptionRow{}
	for rows.Next() {
		var row ToolUsageHostedServerFilterOptionRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan tool usage hosted server filter option row: %w", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) getToolUsageShadowServerFilterOptions(ctx context.Context, arg GetToolUsageSummaryParams) ([]ToolUsageShadowServerFilterOptionRow, error) {
	sb, err := toolUsageBaseSelect(arg,
		"target_id AS server_name",
		"count() AS event_count",
	)
	if err != nil {
		return nil, fmt.Errorf("building tool usage shadow server filter options source: %w", err)
	}
	sb = sb.
		Where(squirrel.Eq{"target_type": ToolUsageTargetTypeShadowMCP}).
		GroupBy("server_name").
		OrderBy("event_count DESC", "server_name ASC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool usage shadow server filter options query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ToolUsageShadowServerFilterOptionRow{}
	for rows.Next() {
		var row ToolUsageShadowServerFilterOptionRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan tool usage shadow server filter option row: %w", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) getToolUsageUserFilterOptions(ctx context.Context, arg GetToolUsageSummaryParams) ([]ToolUsageUserFilterOptionRow, error) {
	sb, err := toolUsageBaseSelect(arg,
		"user_key",
		"user_label",
		"user_kind",
		"count() AS event_count",
	)
	if err != nil {
		return nil, fmt.Errorf("building tool usage user filter options source: %w", err)
	}
	sb = sb.
		Where(squirrel.NotEq{"user_kind": toolUsageUserKindUnknown}).
		GroupBy("user_key", "user_label", "user_kind").
		OrderBy("event_count DESC", "user_label ASC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tool usage user filter options query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ToolUsageUserFilterOptionRow{}
	for rows.Next() {
		var row ToolUsageUserFilterOptionRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan tool usage user filter option row: %w", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func toolUsageBaseSelect(arg GetToolUsageSummaryParams, columns ...string) (squirrel.SelectBuilder, error) {
	cteSQL, cteArgs, err := toolUsageNormalizedEventsCTE(arg)
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}

	return sq.Select(columns...).
		Prefix(cteSQL, cteArgs...).
		From("normalized_events"), nil
}

func toolUsageFilteredSelect(arg GetToolUsageSummaryParams, columns ...string) (squirrel.SelectBuilder, error) {
	sb, err := toolUsageBaseSelect(arg, columns...)
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}

	if len(arg.TargetTypes) > 0 {
		sb = sb.Where(squirrel.Eq{"target_type": arg.TargetTypes})
	}

	if len(arg.HostedToolsetSlugs) > 0 || len(arg.ShadowServerNames) > 0 {
		targetFilters := squirrel.Or{}
		if len(arg.HostedToolsetSlugs) > 0 {
			targetFilters = append(targetFilters, squirrel.And{
				squirrel.Eq{"target_type": ToolUsageTargetTypeHostedMCP},
				squirrel.Eq{"target_id": arg.HostedToolsetSlugs},
			})
		}
		if len(arg.ShadowServerNames) > 0 {
			targetFilters = append(targetFilters, squirrel.And{
				squirrel.Eq{"target_type": ToolUsageTargetTypeShadowMCP},
				squirrel.Eq{"target_id": arg.ShadowServerNames},
			})
		}
		sb = sb.Where(targetFilters)
	}

	if len(arg.UserFilters) > 0 {
		userFilters := squirrel.Or{}
		for _, filter := range arg.UserFilters {
			userFilters = append(userFilters, squirrel.And{
				squirrel.Eq{"user_kind": filter.Kind},
				squirrel.Eq{"user_key": filter.Key},
			})
		}
		sb = sb.Where(userFilters)
	}

	// Direct hosted MCP calls have no hook source (empty string), so requiring a
	// specific hook source naturally excludes them — matching the Tool Logs page.
	if len(arg.HookSources) > 0 {
		sb = sb.Where(squirrel.Eq{"hook_source": arg.HookSources})
	}

	// account_type is carried on trace_summaries and projected through the CTE.
	sb = withAccountTypeFilter(sb, arg.AccountType)

	return sb, nil
}

func toolUsageHostedMatcherArrays(matchers []HostedMCPMatcher) (toolsetSlugs []string, mcpSlugs []string, urlSuffixes []string) {
	toolsetSlugs = make([]string, 0, len(matchers))
	mcpSlugs = make([]string, 0, len(matchers))
	urlSuffixes = make([]string, 0, len(matchers))
	for _, matcher := range matchers {
		if matcher.ToolsetSlug == "" || matcher.McpSlug == "" {
			continue
		}
		toolsetSlugs = append(toolsetSlugs, matcher.ToolsetSlug)
		mcpSlugs = append(mcpSlugs, matcher.McpSlug)
		urlSuffixes = append(urlSuffixes, "/mcp/"+matcher.McpSlug)
	}
	return toolsetSlugs, mcpSlugs, urlSuffixes
}

func toolUsageMCPServerMatcherArrays(matchers []MCPServerMatcher) (sourceIDs []string, targetTypes []string, targetIDs []string, targetLabels []string) {
	sourceIDs = make([]string, 0, len(matchers))
	targetTypes = make([]string, 0, len(matchers))
	targetIDs = make([]string, 0, len(matchers))
	targetLabels = make([]string, 0, len(matchers))
	for _, matcher := range matchers {
		if matcher.SourceID == "" || matcher.TargetType == "" || matcher.TargetID == "" {
			continue
		}
		sourceIDs = append(sourceIDs, matcher.SourceID)
		targetTypes = append(targetTypes, matcher.TargetType)
		targetIDs = append(targetIDs, matcher.TargetID)
		if matcher.TargetLabel != "" {
			targetLabels = append(targetLabels, matcher.TargetLabel)
		} else {
			targetLabels = append(targetLabels, matcher.TargetID)
		}
	}
	return sourceIDs, targetTypes, targetIDs, targetLabels
}

func toolUsageHostedMatchIndexExpr(matchExpr, serverURLExpr string) string {
	matchIndex := "indexOf(?, " + matchExpr + ")"
	urlIndex := "arrayFirstIndex(suffix -> endsWith(" + serverURLExpr + ", suffix), ?)"
	return chMultiIf(
		matchIndex+" > 0", matchIndex,
		urlIndex+" > 0", urlIndex,
		"0",
	)
}

func toolUsageMCPServerMatchIndexExpr(sourceExpr string) string {
	return "indexOf(?, " + sourceExpr + ")"
}

// toolUsageTraceRowsFromSummariesCTE builds the normalized_traces CTE from the
// trace_summaries materialized view (one row per trace) for the common case where no
// free-text query or arbitrary attribute filters are active. It emits the same output
// columns as the raw-log path so ListToolUsageTraces selects from it unchanged. Tool
// calls carry a real trace_id (recorded by the gateway in ToolProxy.Do), so hosted
// MCP, shadow MCP, skill, and local tool events are all present in the view; only
// free-text/custom-attribute search and traceless trigger events need the raw scan.
func toolUsageTraceRowsFromSummariesCTE(arg ListToolUsageTracesParams) (string, []any, error) {
	groupedSB := sq.Select(
		"toString(trace_id) AS g_trace_id",
		"min(start_time_unix_nano) AS event_time_ns",
		"sum(log_count) AS g_log_count",
		"any(gram_urn) AS g_gram_urn",
		"any(tool_name) AS g_tool_name",
		"any(tool_source) AS g_tool_source",
		"max(toolset_slug) AS g_toolset_slug",
		"any(skill_name) AS g_skill_name",
		"any(user_email) AS g_user_email",
		"max(external_user_id) AS g_external_user_id",
		"max(user_id) AS g_user_id",
		"any(hook_source) AS g_hook_source",
		"any(event_source) AS g_event_source",
		"max(mcp_match) AS g_mcp_match",
		"max(mcp_server_url) AS g_mcp_server_url",
		"anyIfMerge(http_status_code) AS g_http_status_code",
		// Reconstruct the hook status deterministically from the highest-severity
		// rank across the trace's summary rows (mirrors the raw-log path), rather
		// than max()-ing each boolean independently.
		"max(hook_status_rank) AS g_hook_status_rank",
		"max(block_reason) AS g_block_reason",
		"max(account_type) AS g_account_type",
	).
		From("(SELECT *, multiIf(has_block = 1, 3, has_error = 1, 2, has_result = 1, 1, 0) AS hook_status_rank FROM trace_summaries)").
		Where("gram_project_id = ?", arg.GramProjectID).
		GroupBy("trace_id").
		Having("min(start_time_unix_nano) >= ?", arg.TimeStart).
		Having("min(start_time_unix_nano) <= ?", arg.TimeEnd).
		Having("((startsWith(g_gram_urn, 'tools:') AND (g_toolset_slug != '' OR g_tool_source != '')) OR (g_event_source = 'hook' AND (g_tool_name != '' OR g_skill_name != '')))")

	groupedSQL, groupedArgs, err := groupedSB.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building tool usage trace summaries source: %w", err)
	}

	sourceSQL := groupedSQL
	sourceArgs := groupedArgs
	hostedToolsetSlugs, hostedMCPSlugs, hostedURLSuffixes := toolUsageHostedMatcherArrays(arg.HostedMCPMatchers)
	hasMatchers := len(hostedToolsetSlugs) > 0
	mcpSourceIDs, mcpTargetTypes, mcpTargetIDs, mcpTargetLabels := toolUsageMCPServerMatcherArrays(arg.MCPServerMatchers)
	hasMCPServerMatchers := len(mcpSourceIDs) > 0
	if hasMatchers || hasMCPServerMatchers {
		columns := []string{"*"}
		prefixArgs := []any{}
		if hasMatchers {
			hostedIndex := toolUsageHostedMatchIndexExpr("g_mcp_match", "g_mcp_server_url")
			columns = append(columns, hostedIndex+" AS hosted_match_index")
			prefixArgs = append(prefixArgs, hostedMCPSlugs, hostedMCPSlugs, hostedURLSuffixes, hostedURLSuffixes)
		}
		if hasMCPServerMatchers {
			columns = append(columns, toolUsageMCPServerMatchIndexExpr("g_tool_source")+" AS mcp_server_match_index")
			prefixArgs = append(prefixArgs, mcpSourceIDs)
		}
		sourceSQL = fmt.Sprintf("SELECT %s FROM (%s)", strings.Join(columns, ", "), groupedSQL)
		sourceArgs = prefixArgs
		sourceArgs = append(sourceArgs, groupedArgs...)
	}

	isSkillCall := "g_skill_name != ''"
	skillLabel := "g_skill_name"
	toolName := chMultiIf(isSkillCall, skillLabel, "g_tool_name")

	var targetType, targetKind, targetID, targetLabel string
	if hasMatchers || hasMCPServerMatchers {
		targetTypeArgs := []string{
			"g_event_source != 'hook' AND g_toolset_slug != ''", "'" + ToolUsageTargetTypeHostedMCP + "'",
		}
		targetKindArgs := []string{
			"g_event_source != 'hook' AND g_toolset_slug != ''", "'" + toolUsageTargetKindServer + "'",
		}
		targetIDArgs := []string{
			"g_event_source != 'hook' AND g_toolset_slug != ''", "g_toolset_slug",
		}
		targetLabelArgs := []string{
			"g_event_source != 'hook' AND g_toolset_slug != ''", "g_toolset_slug",
		}
		if hasMCPServerMatchers {
			mcpServerMatch := "mcp_server_match_index > 0"
			targetTypeArgs = append(targetTypeArgs, mcpServerMatch, "arrayElement(?, mcp_server_match_index)")
			targetKindArgs = append(targetKindArgs, mcpServerMatch, "'"+toolUsageTargetKindServer+"'")
			targetIDArgs = append(targetIDArgs, mcpServerMatch, "arrayElement(?, mcp_server_match_index)")
			targetLabelArgs = append(targetLabelArgs, mcpServerMatch, "arrayElement(?, mcp_server_match_index)")
		}
		if hasMatchers {
			hostedMatch := "hosted_match_index > 0"
			targetTypeArgs = append(targetTypeArgs, hostedMatch, "'"+ToolUsageTargetTypeHostedMCP+"'")
			targetKindArgs = append(targetKindArgs, hostedMatch, "'"+toolUsageTargetKindServer+"'")
			targetIDArgs = append(targetIDArgs, hostedMatch, "arrayElement(?, hosted_match_index)")
			targetLabelArgs = append(targetLabelArgs, hostedMatch, "arrayElement(?, hosted_match_index)")
		}
		targetTypeArgs = append(targetTypeArgs,
			isSkillCall, "'"+ToolUsageTargetTypeSkill+"'",
			"g_tool_source != ''", "'"+ToolUsageTargetTypeShadowMCP+"'",
			"'"+ToolUsageTargetTypeLocalTool+"'",
		)
		targetKindArgs = append(targetKindArgs,
			isSkillCall, "'"+toolUsageTargetKindSkill+"'",
			"g_tool_source != ''", "'"+toolUsageTargetKindServer+"'",
			"'"+toolUsageTargetKindLocalTools+"'",
		)
		targetIDArgs = append(targetIDArgs,
			isSkillCall, skillLabel,
			"g_tool_source != ''", "g_tool_source",
			"'local'",
		)
		targetLabelArgs = append(targetLabelArgs,
			isSkillCall, skillLabel,
			"g_tool_source != ''", "g_tool_source",
			"'Local Tools'",
		)
		targetType = chMultiIf(targetTypeArgs...)
		targetKind = chMultiIf(targetKindArgs...)
		targetID = chMultiIf(targetIDArgs...)
		targetLabel = chMultiIf(targetLabelArgs...)
	} else {
		targetType = chMultiIf(
			"g_event_source != 'hook' AND g_toolset_slug != ''", "'"+ToolUsageTargetTypeHostedMCP+"'",
			isSkillCall, "'"+ToolUsageTargetTypeSkill+"'",
			"g_tool_source != ''", "'"+ToolUsageTargetTypeShadowMCP+"'",
			"'"+ToolUsageTargetTypeLocalTool+"'",
		)
		targetKind = chMultiIf(
			"g_event_source != 'hook' AND g_toolset_slug != ''", "'"+toolUsageTargetKindServer+"'",
			isSkillCall, "'"+toolUsageTargetKindSkill+"'",
			"g_tool_source != ''", "'"+toolUsageTargetKindServer+"'",
			"'"+toolUsageTargetKindLocalTools+"'",
		)
		targetID = chMultiIf(
			"g_event_source != 'hook' AND g_toolset_slug != ''", "g_toolset_slug",
			isSkillCall, skillLabel,
			"g_tool_source != ''", "g_tool_source",
			"'local'",
		)
		targetLabel = chMultiIf(
			"g_event_source != 'hook' AND g_toolset_slug != ''", "g_toolset_slug",
			isSkillCall, skillLabel,
			"g_tool_source != ''", "g_tool_source",
			"'Local Tools'",
		)
	}

	userKey := chFirstNonEmpty("g_user_email", "g_external_user_id", "g_user_id", "'Unknown'")
	userKind := chMultiIf(
		"g_user_email != ''", "'"+toolUsageUserKindEmail+"'",
		"g_external_user_id != ''", "'"+toolUsageUserKindExternalUserID+"'",
		"g_user_id != ''", "'"+toolUsageUserKindUserID+"'",
		"'"+toolUsageUserKindUnknown+"'",
	)
	hookStatus := "if(g_event_source = 'hook', multiIf(g_hook_status_rank = 3, CAST('blocked' AS Nullable(String)), g_hook_status_rank = 2, CAST('failure' AS Nullable(String)), g_hook_status_rank = 1, CAST('success' AS Nullable(String)), CAST('pending' AS Nullable(String))), CAST(NULL AS Nullable(String)))"

	tracesSQL := fmt.Sprintf(`
SELECT
	g_trace_id AS id,
	g_trace_id AS trace_id,
	'trace_id' AS log_group_kind,
	g_trace_id AS log_group_value,
	event_time_ns AS start_time_unix_nano,
	g_log_count AS log_count,
	g_gram_urn AS gram_urn,
	%s AS tool_name,
	%s AS target_type,
	%s AS target_kind,
	%s AS target_id,
	%s AS target_label,
	%s AS user_key,
	%s AS user_label,
	%s AS user_kind,
	nullIf(g_hook_source, '') AS hook_source,
	g_event_source AS event_source,
	g_http_status_code AS http_status_code,
	%s AS hook_status,
	nullIf(g_block_reason, '') AS block_reason,
	nullIf(g_account_type, '') AS account_type
FROM (%s)`,
		toolName,
		targetType,
		targetKind,
		targetID,
		targetLabel,
		userKey,
		userKey,
		userKind,
		hookStatus,
		sourceSQL,
	)

	finalArgs := make([]any, 0, 2+len(sourceArgs))
	if hasMCPServerMatchers {
		finalArgs = append(finalArgs, mcpTargetTypes)
	}
	if hasMCPServerMatchers {
		finalArgs = append(finalArgs, mcpTargetIDs)
	}
	if hasMatchers {
		finalArgs = append(finalArgs, hostedToolsetSlugs)
	}
	if hasMCPServerMatchers {
		finalArgs = append(finalArgs, mcpTargetLabels)
	}
	if hasMatchers {
		finalArgs = append(finalArgs, hostedToolsetSlugs)
	}
	finalArgs = append(finalArgs, sourceArgs...)

	return "WITH normalized_traces AS (" + tracesSQL + ")", finalArgs, nil
}

func toolUsageTraceRowsCTE(arg ListToolUsageTracesParams) (string, []any, error) {
	// Fast path: when no free-text query or arbitrary attribute filters are active,
	// serve the trace list from the trace_summaries materialized view. Free-text
	// search, custom-attribute filters, and (traceless) trigger events fall through
	// to the raw telemetry_logs scan below.
	if arg.Query == "" && len(arg.Filters) == 0 {
		return toolUsageTraceRowsFromSummariesCTE(arg)
	}

	conversationID := chFirstNonEmpty(chAttr("gen_ai.conversation.id"), "toString(attributes.`genai.conversation.id`)")
	triggerInstanceID := chAttr("gram.trigger.instance_id")
	toolCallArguments := chFirstNonEmpty(
		chAttr("gen_ai.tool.call.arguments"),
		"toString(attributes.`gen_ai.tool.call.arguments`)",
		"JSONExtractString(toString(attributes), 'gen_ai.tool.call.arguments')",
	)
	toolCallSkillName := "JSONExtractString(" + toolCallArguments + ", 'skill')"
	rawSB := sq.Select(
		"id AS log_id",
		"time_unix_nano",
		"trace_id",
		"gram_urn",
		"event_source",
		"toolset_slug",
		"tool_name AS raw_tool_name",
		"tool_source",
		"skill_name",
		"user_email",
		"external_user_id",
		"user_id",
		"account_type",
		chAttr("gram.hook.source")+" AS hook_source",
		chAttr("gen_ai.tool.call.result")+" AS tool_result",
		chAttr("gram.hook.error")+" AS hook_error",
		chAttr("gram.hook.block_reason")+" AS block_reason",
		toolCallArguments+" AS tool_call_arguments",
		chAttr("gram.mcp.match")+" AS mcp_match",
		chAttr("gram.mcp.server_url")+" AS mcp_server_url",
		chAttr("http.response.status_code")+" AS http_status_code_raw",
		conversationID+" AS conversation_id",
		triggerInstanceID+" AS trigger_instance_id",
		chAttr("gram.trigger.event_id")+" AS trigger_event_id",
		chAttr("gram.trigger.correlation_id")+" AS trigger_correlation_id",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd)

	sourceCondition := "((startsWith(gram_urn, 'tools:') AND (toolset_slug != '' OR tool_source != '')) OR (event_source = 'hook' AND (tool_name != '' OR skill_name != '' OR " + toolCallSkillName + " != '')))"
	includeTriggerRows := arg.Query != ""
	for _, filter := range arg.Filters {
		if strings.HasPrefix(filter.Path, "gram.trigger.") {
			includeTriggerRows = true
			break
		}
	}
	if includeTriggerRows {
		sourceCondition = "(" + sourceCondition + " OR event_source = 'trigger')"
	}
	rawSB = rawSB.Where(sourceCondition)

	if arg.Query != "" {
		rawSB = rawSB.Where(
			"(position(gram_urn, ?) > 0 OR position("+conversationID+", ?) > 0 OR position("+triggerInstanceID+", ?) > 0)",
			arg.Query,
			arg.Query,
			arg.Query,
		)
	}

	for _, filter := range arg.Filters {
		if !validJSONPath.MatchString(filter.Path) {
			continue
		}
		// http.response.status_code is a per-trace status, applied at the aggregated
		// trace level in ListToolUsageTraces — never pushed down to raw rows here. See
		// toolUsageHTTPStatusPath.
		if filter.Path == toolUsageHTTPStatusPath {
			continue
		}
		pred := filter.Predicate(resolveAttributeColumn(filter.Path))
		if pred != nil {
			rawSB = rawSB.Where(pred)
		}
	}

	rawSQL, rawArgs, err := rawSB.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building tool usage trace raw source: %w", err)
	}

	hostedToolsetSlugs, hostedMCPSlugs, hostedURLSuffixes := toolUsageHostedMatcherArrays(arg.HostedMCPMatchers)
	mcpSourceIDs, mcpTargetTypes, mcpTargetIDs, mcpTargetLabels := toolUsageMCPServerMatcherArrays(arg.MCPServerMatchers)
	sourceSQL := rawSQL
	sourceArgs := rawArgs
	if len(hostedToolsetSlugs) > 0 || len(mcpSourceIDs) > 0 {
		columns := []string{"*"}
		prefixArgs := []any{}
		if len(hostedToolsetSlugs) > 0 {
			hostedIndex := toolUsageHostedMatchIndexExpr("mcp_match", "mcp_server_url")
			columns = append(columns, hostedIndex+" AS hosted_match_index")
			prefixArgs = append(prefixArgs, hostedMCPSlugs, hostedMCPSlugs, hostedURLSuffixes, hostedURLSuffixes)
		}
		if len(mcpSourceIDs) > 0 {
			columns = append(columns, toolUsageMCPServerMatchIndexExpr("tool_source")+" AS mcp_server_match_index")
			prefixArgs = append(prefixArgs, mcpSourceIDs)
		}
		sourceSQL = fmt.Sprintf("SELECT %s FROM (%s)", strings.Join(columns, ", "), rawSQL)
		sourceArgs = prefixArgs
		sourceArgs = append(sourceArgs, rawArgs...)
	}

	userKind := chMultiIf(
		"user_email != ''", "'"+toolUsageUserKindEmail+"'",
		"external_user_id != ''", "'"+toolUsageUserKindExternalUserID+"'",
		"user_id != ''", "'"+toolUsageUserKindUserID+"'",
		"'"+toolUsageUserKindUnknown+"'",
	)
	userKey := chFirstNonEmpty("user_email", "external_user_id", "user_id", "'Unknown'")
	logGroupKind := chMultiIf(
		"trace_id != ''", "'trace_id'",
		"trigger_correlation_id != ''", "'correlation_id'",
		"trigger_event_id != ''", "'trigger_event_id'",
		"'log_id'",
	)
	logGroupValue := chMultiIf(
		"trace_id != ''", "toString(trace_id)",
		"trigger_correlation_id != ''", "trigger_correlation_id",
		"trigger_event_id != ''", "trigger_event_id",
		"toString(log_id)",
	)
	eventSkillName := chFirstNonEmpty("skill_name", "JSONExtractString(tool_call_arguments, 'skill')")
	skillName := "anyIf(" + eventSkillName + ", " + eventSkillName + " != '') OVER (PARTITION BY " + logGroupKind + ", " + logGroupValue + ")"
	hasSkillTool := "max(toUInt8(raw_tool_name = 'Skill')) OVER (PARTITION BY " + logGroupKind + ", " + logGroupValue + ") = 1"
	isSkillCall := "(" + hasSkillTool + " OR " + skillName + " != '')"
	skillLabel := chFirstNonEmpty(skillName, "''")
	targetType := chMultiIf(
		"event_source != 'hook' AND toolset_slug != ''", "'"+ToolUsageTargetTypeHostedMCP+"'",
		isSkillCall, "'"+ToolUsageTargetTypeSkill+"'",
		"tool_source != ''", "'"+ToolUsageTargetTypeShadowMCP+"'",
		"'"+ToolUsageTargetTypeLocalTool+"'",
	)
	targetKind := chMultiIf(
		"event_source != 'hook' AND toolset_slug != ''", "'"+toolUsageTargetKindServer+"'",
		isSkillCall, "'"+toolUsageTargetKindSkill+"'",
		"tool_source != ''", "'"+toolUsageTargetKindServer+"'",
		"'"+toolUsageTargetKindLocalTools+"'",
	)
	targetID := chMultiIf(
		"event_source != 'hook' AND toolset_slug != ''", "toolset_slug",
		isSkillCall, skillLabel,
		"tool_source != ''", "tool_source",
		"'local'",
	)
	targetLabel := chMultiIf(
		"event_source != 'hook' AND toolset_slug != ''", "toolset_slug",
		isSkillCall, skillLabel,
		"tool_source != ''", "tool_source",
		"'Local Tools'",
	)
	if len(hostedToolsetSlugs) > 0 || len(mcpSourceIDs) > 0 {
		targetTypeArgs := []string{
			"event_source != 'hook' AND toolset_slug != ''", "'" + ToolUsageTargetTypeHostedMCP + "'",
		}
		targetKindArgs := []string{
			"event_source != 'hook' AND toolset_slug != ''", "'" + toolUsageTargetKindServer + "'",
		}
		targetIDArgs := []string{
			"event_source != 'hook' AND toolset_slug != ''", "toolset_slug",
		}
		targetLabelArgs := []string{
			"event_source != 'hook' AND toolset_slug != ''", "toolset_slug",
		}
		if len(mcpSourceIDs) > 0 {
			mcpServerMatchCondition := "mcp_server_match_index > 0"
			targetTypeArgs = append(targetTypeArgs, mcpServerMatchCondition, "arrayElement(?, mcp_server_match_index)")
			targetKindArgs = append(targetKindArgs, mcpServerMatchCondition, "'"+toolUsageTargetKindServer+"'")
			targetIDArgs = append(targetIDArgs, mcpServerMatchCondition, "arrayElement(?, mcp_server_match_index)")
			targetLabelArgs = append(targetLabelArgs, mcpServerMatchCondition, "arrayElement(?, mcp_server_match_index)")
		}
		if len(hostedToolsetSlugs) > 0 {
			hostedMatchCondition := "hosted_match_index > 0"
			targetTypeArgs = append(targetTypeArgs, hostedMatchCondition, "'"+ToolUsageTargetTypeHostedMCP+"'")
			targetKindArgs = append(targetKindArgs, hostedMatchCondition, "'"+toolUsageTargetKindServer+"'")
			targetIDArgs = append(targetIDArgs, hostedMatchCondition, "arrayElement(?, hosted_match_index)")
			targetLabelArgs = append(targetLabelArgs, hostedMatchCondition, "arrayElement(?, hosted_match_index)")
		}
		targetTypeArgs = append(targetTypeArgs,
			isSkillCall, "'"+ToolUsageTargetTypeSkill+"'",
			"tool_source != ''", "'"+ToolUsageTargetTypeShadowMCP+"'",
			"'"+ToolUsageTargetTypeLocalTool+"'",
		)
		targetKindArgs = append(targetKindArgs,
			isSkillCall, "'"+toolUsageTargetKindSkill+"'",
			"tool_source != ''", "'"+toolUsageTargetKindServer+"'",
			"'"+toolUsageTargetKindLocalTools+"'",
		)
		targetIDArgs = append(targetIDArgs,
			isSkillCall, skillLabel,
			"tool_source != ''", "tool_source",
			"'local'",
		)
		targetLabelArgs = append(targetLabelArgs,
			isSkillCall, skillLabel,
			"tool_source != ''", "tool_source",
			"'Local Tools'",
		)
		targetType = chMultiIf(targetTypeArgs...)
		targetKind = chMultiIf(targetKindArgs...)
		targetID = chMultiIf(targetIDArgs...)
		targetLabel = chMultiIf(targetLabelArgs...)
	}
	normalizedSQL := fmt.Sprintf(`
SELECT
	log_id,
	time_unix_nano,
	trace_id,
	%s AS log_group_kind,
	%s AS log_group_value,
	gram_urn,
	%s AS tool_name,
	%s AS target_type,
	%s AS target_kind,
	%s AS target_id,
	%s AS target_label,
	%s AS user_key,
	%s AS user_label,
	%s AS user_kind,
	nullIf(hook_source, '') AS hook_source,
	event_source,
	toInt32OrNull(http_status_code_raw) AS http_status_code,
	if(event_source = 'hook', CAST(multiIf(block_reason != '', 'blocked', hook_error != '', 'failure', tool_result != '', 'success', 'pending') AS Nullable(String)), CAST(NULL AS Nullable(String))) AS hook_status,
	if(event_source = 'hook', CAST(multiIf(block_reason != '', 3, hook_error != '', 2, tool_result != '', 1, 0) AS Nullable(UInt8)), CAST(NULL AS Nullable(UInt8))) AS hook_status_rank,
	nullIf(block_reason, '') AS block_reason,
	account_type
FROM (%s)`, logGroupKind, logGroupValue, chMultiIf(isSkillCall, skillLabel, "raw_tool_name"), targetType, targetKind, targetID, targetLabel, userKey, userKey, userKind, sourceSQL)

	normalizedArgs := make([]any, 0, 2+len(sourceArgs))
	if len(mcpSourceIDs) > 0 {
		normalizedArgs = append(normalizedArgs, mcpTargetTypes)
	}
	if len(mcpSourceIDs) > 0 {
		normalizedArgs = append(normalizedArgs, mcpTargetIDs)
	}
	if len(hostedToolsetSlugs) > 0 {
		normalizedArgs = append(normalizedArgs, hostedToolsetSlugs)
	}
	if len(mcpSourceIDs) > 0 {
		normalizedArgs = append(normalizedArgs, mcpTargetLabels)
	}
	if len(hostedToolsetSlugs) > 0 {
		normalizedArgs = append(normalizedArgs, hostedToolsetSlugs)
	}
	normalizedArgs = append(normalizedArgs, sourceArgs...)

	traceSQL := `
WITH raw_normalized_events AS (` + normalizedSQL + `),
normalized_traces AS (
	SELECT
		concat(log_group_kind, ':', log_group_value, ':', target_type, ':', target_id, ':', tool_name) AS id,
		if(log_group_kind = 'trace_id', log_group_value, '') AS trace_id,
		log_group_kind,
		log_group_value,
		min(time_unix_nano) AS start_time_unix_nano,
		count() AS log_count,
		any(gram_urn) AS gram_urn,
		tool_name,
		target_type,
		target_kind,
		target_id,
		target_label,
		user_key,
		user_label,
		user_kind,
		any(hook_source) AS hook_source,
		any(event_source) AS event_source,
		max(http_status_code) AS http_status_code,
		multiIf(
			ifNull(max(hook_status_rank), toUInt8(255)) = 3, CAST('blocked' AS Nullable(String)),
			ifNull(max(hook_status_rank), toUInt8(255)) = 2, CAST('failure' AS Nullable(String)),
			ifNull(max(hook_status_rank), toUInt8(255)) = 1, CAST('success' AS Nullable(String)),
			ifNull(max(hook_status_rank), toUInt8(255)) = 0, CAST('pending' AS Nullable(String)),
			CAST(NULL AS Nullable(String))
		) AS hook_status,
		nullIf(anyIf(ifNull(block_reason, ''), ifNull(hook_status_rank, toUInt8(0)) = 3 AND ifNull(block_reason, '') != ''), '') AS block_reason,
		nullIf(any(account_type), '') AS account_type
	FROM raw_normalized_events
	GROUP BY log_group_kind, log_group_value, target_type, target_kind, target_id, target_label, tool_name, user_kind, user_key, user_label
)`

	return traceSQL, normalizedArgs, nil
}

func toolUsageNormalizedEventsCTE(arg GetToolUsageSummaryParams) (string, []any, error) {
	// Served from the trace_summaries materialized view (one row per trace) instead
	// of scanning raw telemetry_logs. Tool calls carry a real trace_id (recorded by
	// the gateway in ToolProxy.Do), so hosted MCP, shadow MCP, skill, and local tool
	// events all land in trace_summaries and can be classified here without a full
	// log scan. This path never carries free-text/arbitrary-attribute filters, so it
	// always reads from the summary view.
	// Each branch GROUPs BY trace_id over the AggregatingMergeTree, then a wrapping
	// SELECT classifies the per-trace row. The grouped aggregate columns are aliased
	// with a "g_" prefix that does NOT collide with the underlying trace_summaries
	// column names. This matters: ClickHouse merges a subquery whose aggregate alias
	// shadows a base column (e.g. `any(gram_urn) AS gram_urn`) back into the enclosing
	// aggregate, which nests any()/sum() and fails with ILLEGAL_AGGREGATION once a
	// caller (uniqExact(tool_name), sum(success), ...) aggregates over normalized_events.
	userKind := chMultiIf(
		"g_user_email != ''", "'"+toolUsageUserKindEmail+"'",
		"g_external_user_id != ''", "'"+toolUsageUserKindExternalUserID+"'",
		"g_user_id != ''", "'"+toolUsageUserKindUserID+"'",
		"'"+toolUsageUserKindUnknown+"'",
	)
	userKey := chFirstNonEmpty("g_user_email", "g_external_user_id", "g_user_id", "'Unknown'")

	directGroupedSB := sq.Select(
		"min(start_time_unix_nano) AS event_time_ns",
		"max(toolset_slug) AS g_toolset_slug",
		"any(tool_source) AS g_tool_source",
		"any(tool_name) AS g_tool_name",
		"any(gram_urn) AS g_gram_urn",
		"any(user_email) AS g_user_email",
		"max(external_user_id) AS g_external_user_id",
		"max(user_id) AS g_user_id",
		"ifNull(anyIfMerge(http_status_code), 0) AS g_http_status_code",
		"max(account_type) AS g_account_type",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		GroupBy("trace_id").
		Having("min(start_time_unix_nano) >= ?", arg.TimeStart).
		Having("min(start_time_unix_nano) <= ?", arg.TimeEnd).
		Having("any(event_source) != 'hook'").
		Having("startsWith(g_gram_urn, 'tools:')").
		Having("(g_toolset_slug != '' OR g_tool_source != '')")

	hookGroupedSB := sq.Select(
		"min(start_time_unix_nano) AS event_time_ns",
		"any(tool_name) AS g_tool_name",
		"any(tool_source) AS g_tool_source",
		"any(user_email) AS g_user_email",
		"max(external_user_id) AS g_external_user_id",
		"max(user_id) AS g_user_id",
		"any(skill_name) AS g_skill_name",
		"max(mcp_match) AS g_mcp_match",
		"max(mcp_server_url) AS g_mcp_server_url",
		"any(hook_source) AS g_hook_source",
		"max(has_result) AS g_has_result",
		"max(has_error) AS g_has_error",
		"max(account_type) AS g_account_type",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		GroupBy("trace_id").
		Having("min(start_time_unix_nano) >= ?", arg.TimeStart).
		Having("min(start_time_unix_nano) <= ?", arg.TimeEnd).
		Having("any(event_source) = 'hook'").
		Having("(g_tool_name != '' OR g_skill_name != '')")

	directGroupedSQL, directGroupedArgs, err := directGroupedSB.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building direct tool usage source: %w", err)
	}

	hostedToolsetSlugs, hostedMCPSlugs, hostedURLSuffixes := toolUsageHostedMatcherArrays(arg.HostedMCPMatchers)
	mcpSourceIDs, mcpTargetTypes, mcpTargetIDs, mcpTargetLabels := toolUsageMCPServerMatcherArrays(arg.MCPServerMatchers)

	directSourceSQL := directGroupedSQL
	directSourceArgs := directGroupedArgs
	if len(mcpSourceIDs) > 0 {
		directSourceSQL = fmt.Sprintf("SELECT *, %s AS mcp_server_match_index FROM (%s)", toolUsageMCPServerMatchIndexExpr("g_tool_source"), directGroupedSQL)
		directSourceArgs = []any{mcpSourceIDs}
		directSourceArgs = append(directSourceArgs, directGroupedArgs...)
	}

	directTargetType := chMultiIf(
		"g_toolset_slug != ''", "'"+ToolUsageTargetTypeHostedMCP+"'",
		"g_tool_source != ''", "'"+ToolUsageTargetTypeHostedMCP+"'",
		"'"+ToolUsageTargetTypeLocalTool+"'",
	)
	directTargetID := chMultiIf(
		"g_toolset_slug != ''", "g_toolset_slug",
		"g_tool_source != ''", "g_tool_source",
		"'local'",
	)
	directTargetLabel := chMultiIf(
		"g_toolset_slug != ''", "g_toolset_slug",
		"g_tool_source != ''", "g_tool_source",
		"'Local Tools'",
	)
	if len(mcpSourceIDs) > 0 {
		mcpServerMatchCondition := "mcp_server_match_index > 0"
		directTargetType = chMultiIf(
			"g_toolset_slug != ''", "'"+ToolUsageTargetTypeHostedMCP+"'",
			mcpServerMatchCondition, "arrayElement(?, mcp_server_match_index)",
			"g_tool_source != ''", "'"+ToolUsageTargetTypeHostedMCP+"'",
			"'"+ToolUsageTargetTypeLocalTool+"'",
		)
		directTargetID = chMultiIf(
			"g_toolset_slug != ''", "g_toolset_slug",
			mcpServerMatchCondition, "arrayElement(?, mcp_server_match_index)",
			"g_tool_source != ''", "g_tool_source",
			"'local'",
		)
		directTargetLabel = chMultiIf(
			"g_toolset_slug != ''", "g_toolset_slug",
			mcpServerMatchCondition, "arrayElement(?, mcp_server_match_index)",
			"g_tool_source != ''", "g_tool_source",
			"'Local Tools'",
		)
	}

	directSQL := fmt.Sprintf(`
SELECT
	event_time_ns,
	%s AS target_type,
	'%s' AS target_kind,
	%s AS target_id,
	%s AS target_label,
	%s AS tool_name,
	%s AS user_key,
	%s AS user_label,
	%s AS user_kind,
	toUInt8(g_http_status_code >= 200 AND g_http_status_code < 400) AS success,
	toUInt8(g_http_status_code >= 400) AS failure,
	'' AS hook_source,
	g_account_type AS account_type
FROM (%s)`,
		directTargetType,
		toolUsageTargetKindServer,
		directTargetID,
		directTargetLabel,
		chFirstNonEmpty("g_tool_name", "g_gram_urn"),
		userKey,
		userKey,
		userKind,
		directSourceSQL,
	)

	hookGroupedSQL, hookGroupedArgs, err := hookGroupedSB.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building hook tool usage source: %w", err)
	}

	hookSourceSQL := hookGroupedSQL
	hookSourceArgs := hookGroupedArgs
	if len(hostedToolsetSlugs) > 0 || len(mcpSourceIDs) > 0 {
		columns := []string{"*"}
		prefixArgs := []any{}
		if len(hostedToolsetSlugs) > 0 {
			hostedIndex := toolUsageHostedMatchIndexExpr("g_mcp_match", "g_mcp_server_url")
			columns = append(columns, hostedIndex+" AS hosted_match_index")
			prefixArgs = append(prefixArgs, hostedMCPSlugs, hostedMCPSlugs, hostedURLSuffixes, hostedURLSuffixes)
		}
		if len(mcpSourceIDs) > 0 {
			columns = append(columns, toolUsageMCPServerMatchIndexExpr("g_tool_source")+" AS mcp_server_match_index")
			prefixArgs = append(prefixArgs, mcpSourceIDs)
		}
		hookSourceSQL = fmt.Sprintf("SELECT %s FROM (%s)", strings.Join(columns, ", "), hookGroupedSQL)
		hookSourceArgs = prefixArgs
		hookSourceArgs = append(hookSourceArgs, hookGroupedArgs...)
	}

	hookTargetType := chMultiIf(
		"g_skill_name != ''", "'"+ToolUsageTargetTypeSkill+"'",
		"g_tool_source != ''", "'"+ToolUsageTargetTypeShadowMCP+"'",
		"'"+ToolUsageTargetTypeLocalTool+"'",
	)
	hookTargetKind := chMultiIf(
		"g_skill_name != ''", "'"+toolUsageTargetKindSkill+"'",
		"g_tool_source != ''", "'"+toolUsageTargetKindServer+"'",
		"'"+toolUsageTargetKindLocalTools+"'",
	)
	hookTargetID := chMultiIf(
		"g_skill_name != ''", "g_skill_name",
		"g_tool_source != ''", "g_tool_source",
		"'local'",
	)
	hookTargetLabel := chMultiIf(
		"g_skill_name != ''", "g_skill_name",
		"g_tool_source != ''", "g_tool_source",
		"'Local Tools'",
	)
	if len(hostedToolsetSlugs) > 0 || len(mcpSourceIDs) > 0 {
		hookTargetTypeArgs := []string{}
		hookTargetKindArgs := []string{}
		hookTargetIDArgs := []string{}
		hookTargetLabelArgs := []string{}
		if len(mcpSourceIDs) > 0 {
			mcpServerMatchCondition := "mcp_server_match_index > 0"
			hookTargetTypeArgs = append(hookTargetTypeArgs, mcpServerMatchCondition, "arrayElement(?, mcp_server_match_index)")
			hookTargetKindArgs = append(hookTargetKindArgs, mcpServerMatchCondition, "'"+toolUsageTargetKindServer+"'")
			hookTargetIDArgs = append(hookTargetIDArgs, mcpServerMatchCondition, "arrayElement(?, mcp_server_match_index)")
			hookTargetLabelArgs = append(hookTargetLabelArgs, mcpServerMatchCondition, "arrayElement(?, mcp_server_match_index)")
		}
		if len(hostedToolsetSlugs) > 0 {
			hostedMatchCondition := "hosted_match_index > 0"
			hookTargetTypeArgs = append(hookTargetTypeArgs, hostedMatchCondition, "'"+ToolUsageTargetTypeHostedMCP+"'")
			hookTargetKindArgs = append(hookTargetKindArgs, hostedMatchCondition, "'"+toolUsageTargetKindServer+"'")
			hookTargetIDArgs = append(hookTargetIDArgs, hostedMatchCondition, "arrayElement(?, hosted_match_index)")
			hookTargetLabelArgs = append(hookTargetLabelArgs, hostedMatchCondition, "arrayElement(?, hosted_match_index)")
		}
		hookTargetTypeArgs = append(hookTargetTypeArgs,
			"g_skill_name != ''", "'"+ToolUsageTargetTypeSkill+"'",
			"g_tool_source != ''", "'"+ToolUsageTargetTypeShadowMCP+"'",
			"'"+ToolUsageTargetTypeLocalTool+"'",
		)
		hookTargetKindArgs = append(hookTargetKindArgs,
			"g_skill_name != ''", "'"+toolUsageTargetKindSkill+"'",
			"g_tool_source != ''", "'"+toolUsageTargetKindServer+"'",
			"'"+toolUsageTargetKindLocalTools+"'",
		)
		hookTargetIDArgs = append(hookTargetIDArgs,
			"g_skill_name != ''", "g_skill_name",
			"g_tool_source != ''", "g_tool_source",
			"'local'",
		)
		hookTargetLabelArgs = append(hookTargetLabelArgs,
			"g_skill_name != ''", "g_skill_name",
			"g_tool_source != ''", "g_tool_source",
			"'Local Tools'",
		)
		hookTargetType = chMultiIf(hookTargetTypeArgs...)
		hookTargetKind = chMultiIf(hookTargetKindArgs...)
		hookTargetID = chMultiIf(hookTargetIDArgs...)
		hookTargetLabel = chMultiIf(hookTargetLabelArgs...)
	}
	hookToolName := chMultiIf("g_skill_name != ''", "g_skill_name", "g_tool_name")

	hookSQL := fmt.Sprintf(`
SELECT
	event_time_ns,
	%s AS target_type,
	%s AS target_kind,
	%s AS target_id,
	%s AS target_label,
	%s AS tool_name,
	%s AS user_key,
	%s AS user_label,
	%s AS user_kind,
	toUInt8(g_has_result = 1 AND g_has_error = 0) AS success,
	toUInt8(g_has_error = 1) AS failure,
	g_hook_source AS hook_source,
	g_account_type AS account_type
FROM (%s)`, hookTargetType, hookTargetKind, hookTargetID, hookTargetLabel, hookToolName, userKey, userKey, userKind, hookSourceSQL)

	directArgs := make([]any, 0, 3+len(directSourceArgs))
	if len(mcpSourceIDs) > 0 {
		directArgs = append(directArgs, mcpTargetTypes, mcpTargetIDs, mcpTargetLabels)
	}
	directArgs = append(directArgs, directSourceArgs...)

	hookArgs := make([]any, 0, 5+len(hookSourceArgs))
	if len(mcpSourceIDs) > 0 {
		hookArgs = append(hookArgs, mcpTargetTypes, mcpTargetIDs)
	}
	if len(hostedToolsetSlugs) > 0 {
		hookArgs = append(hookArgs, hostedToolsetSlugs)
	}
	if len(mcpSourceIDs) > 0 {
		hookArgs = append(hookArgs, mcpTargetLabels)
	}
	if len(hostedToolsetSlugs) > 0 {
		hookArgs = append(hookArgs, hostedToolsetSlugs)
	}
	hookArgs = append(hookArgs, hookSourceArgs...)

	args := make([]any, 0, len(directArgs)+len(hookArgs))
	args = append(args, directArgs...)
	args = append(args, hookArgs...)

	return "WITH normalized_events AS (" + directSQL + " UNION ALL " + hookSQL + ")", args, nil
}

func nonZeroLimit(value, fallback uint64) uint64 {
	if value == 0 {
		return fallback
	}
	return value
}

// GetHooksSummaryParams defines the parameters for getting hooks server summary.
type GetHooksSummaryParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	Filters        []AttributeFilter
	TypesToInclude []string
}

// GetHooksSummary retrieves aggregated hooks metrics grouped by server.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetHooksSummary(ctx context.Context, arg GetHooksSummaryParams) ([]HooksServerSummaryRow, error) {
	sb := sq.Select(
		"if(tool_source = '', 'local', tool_source) as server_name",
		"count(*) as event_count",
		"uniqExact(tool_name) as unique_tools",
		"sum(if(has_result = 1 AND has_error = 0, 1, 0)) as success_count",
		"sumIf(has_error, has_error = 1) as failure_count",
		"failure_count / greatest(success_count + failure_count, 1) as failure_rate",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("event_source = 'hook'").
		Where("start_time_unix_nano >= ?", arg.TimeStart).
		Where("start_time_unix_nano <= ?", arg.TimeEnd)

	sb = applyHookFiltersToBuilder(sb, arg.Filters, arg.TypesToInclude)

	sb = sb.GroupBy("server_name").
		OrderBy("event_count DESC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building hooks summary query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []HooksServerSummaryRow
	for rows.Next() {
		var summary HooksServerSummaryRow
		if err = rows.ScanStruct(&summary); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		summaries = append(summaries, summary)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return summaries, nil
}

// GetHooksSessionCountParams defines the parameters for getting unique session count.
type GetHooksSessionCountParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	Filters        []AttributeFilter
	TypesToInclude []string
}

// GetHooksSessionCount retrieves the count of unique sessions for hooks.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetHooksSessionCount(ctx context.Context, arg GetHooksSessionCountParams) (int64, error) {
	sb := sq.Select("uniqExact(toString(attributes.`genai.conversation.id`)) as session_count").
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("event_source = 'hook'").
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd)
	sb = applyHookFiltersToBuilder(sb, arg.Filters, arg.TypesToInclude)

	query, args, err := sb.ToSql()
	if err != nil {
		return 0, fmt.Errorf("building hooks session count query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var count uint64
	if rows.Next() {
		if err = rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("error scanning session count: %w", err)
		}
	}

	if err = rows.Err(); err != nil {
		return 0, err
	}

	return int64(count), nil
}

// HooksUserSummaryRow contains aggregated hooks metrics for a single user.
type HooksUserSummaryRow struct {
	UserEmail    string  `ch:"user_email"`
	EventCount   uint64  `ch:"event_count"`
	UniqueTools  uint64  `ch:"unique_tools"`
	SuccessCount uint64  `ch:"success_count"`
	FailureCount uint64  `ch:"failure_count"`
	FailureRate  float64 `ch:"failure_rate"`
}

// GetHooksUserSummaryParams defines the parameters for getting hooks user summary.
type GetHooksUserSummaryParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	Filters        []AttributeFilter
	TypesToInclude []string
}

// GetHooksUserSummary retrieves aggregated hooks metrics grouped by user.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetHooksUserSummary(ctx context.Context, arg GetHooksUserSummaryParams) ([]HooksUserSummaryRow, error) {
	sb := sq.Select(
		"if(user_email = '', 'Unknown', user_email) as user_email",
		"count(*) as event_count",
		"uniqExact(tool_name) as unique_tools",
		"sum(if(has_result = 1 AND has_error = 0, 1, 0)) as success_count",
		"sumIf(has_error, has_error = 1) as failure_count",
		"failure_count / greatest(success_count + failure_count, 1) as failure_rate",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("event_source = 'hook'").
		Where("start_time_unix_nano >= ?", arg.TimeStart).
		Where("start_time_unix_nano <= ?", arg.TimeEnd)

	sb = applyHookFiltersToBuilder(sb, arg.Filters, arg.TypesToInclude)

	sb = sb.GroupBy("user_email").
		OrderBy("event_count DESC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building hooks user summary query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []HooksUserSummaryRow
	for rows.Next() {
		var summary HooksUserSummaryRow
		if err = rows.ScanStruct(&summary); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		summaries = append(summaries, summary)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return summaries, nil
}

// SkillSummaryRow contains aggregated skills metrics for a single skill.
type SkillSummaryRow struct {
	SkillName   string `ch:"skill_name"`
	UseCount    uint64 `ch:"use_count"`
	UniqueUsers uint64 `ch:"unique_users"`
}

// GetSkillsSummaryParams defines the parameters for getting skills summary.
type GetSkillsSummaryParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	Filters        []AttributeFilter
	TypesToInclude []string
}

// GetSkillsSummary retrieves aggregated skills usage metrics.
// Skills are identified by skill_name in trace_summaries.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetSkillsSummary(ctx context.Context, arg GetSkillsSummaryParams) ([]SkillSummaryRow, error) {
	sb := sq.Select(
		"skill_name",
		"count(*) as use_count",
		"uniqExact(user_email) as unique_users",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("start_time_unix_nano >= ?", arg.TimeStart).
		Where("start_time_unix_nano <= ?", arg.TimeEnd).
		Where("skill_name != ''")

	sb = applyHookFiltersToBuilder(sb, arg.Filters, arg.TypesToInclude)

	sb = sb.GroupBy("skill_name").
		OrderBy("use_count DESC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building skills summary query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []SkillSummaryRow
	for rows.Next() {
		var summary SkillSummaryRow
		if err = rows.ScanStruct(&summary); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		summaries = append(summaries, summary)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return summaries, nil
}

// SkillBreakdownRow contains per-(skill, user) aggregated counts.
type SkillBreakdownRow struct {
	SkillName string `ch:"skill_name"`
	UserEmail string `ch:"user_email"`
	UseCount  uint64 `ch:"use_count"`
}

// GetSkillBreakdownParams defines parameters for getting per-user skill breakdown.
type GetSkillBreakdownParams struct {
	GramProjectID string
	TimeStart     int64
	TimeEnd       int64
	Filters       []AttributeFilter
}

// GetSkillBreakdown retrieves per-(skill, user) usage counts.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetSkillBreakdown(ctx context.Context, arg GetSkillBreakdownParams) ([]SkillBreakdownRow, error) {
	sb := sq.Select("skill_name", "user_email", "count(*) as use_count").
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("event_source = 'hook'").
		Where("tool_name = 'Skill'").
		Where("start_time_unix_nano >= ?", arg.TimeStart).
		Where("start_time_unix_nano <= ?", arg.TimeEnd).
		Where("skill_name != ''")

	// Apply attribute filters (user, server) but not type filters — skill type is hardcoded above.
	sb = applyHookFiltersToBuilder(sb, arg.Filters, nil)
	sb = sb.GroupBy("skill_name", "user_email").OrderBy("skill_name", "use_count DESC").
		Limit(10000) // Defensive cap

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building skill breakdown query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SkillBreakdownRow
	for rows.Next() {
		var row SkillBreakdownRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan skill breakdown row: %w", err)
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// HooksBreakdownRow contains cross-dimensional aggregated counts for a unique (user, server, source, tool) combination.
type HooksBreakdownRow struct {
	UserEmail    string `ch:"user_email"`
	ServerName   string `ch:"server_name"`
	HookSource   string `ch:"hook_source"`
	ToolName     string `ch:"tool_name"`
	EventCount   uint64 `ch:"event_count"`
	FailureCount uint64 `ch:"failure_count"`
}

// GetHooksBreakdownParams defines the parameters for the cross-dimensional hooks breakdown query.
type GetHooksBreakdownParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	Filters        []AttributeFilter
	TypesToInclude []string
}

// GetHooksBreakdown retrieves cross-dimensional hook event counts grouped by (user, server, hook_source, tool).
// This powers bar charts in the analytics dashboard without being limited to paginated trace data.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetHooksBreakdown(ctx context.Context, arg GetHooksBreakdownParams) ([]HooksBreakdownRow, error) {
	sb := sq.Select(
		"if(user_email = '', 'Unknown', user_email) as user_email",
		"if(tool_source = '', 'local', tool_source) as server_name",
		"hook_source",
		"tool_name",
		"count(*) as event_count",
		"sumIf(has_error, has_error = 1) as failure_count",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("event_source = 'hook'").
		Where("start_time_unix_nano >= ?", arg.TimeStart).
		Where("start_time_unix_nano <= ?", arg.TimeEnd)

	sb = applyHookFiltersToBuilder(sb, arg.Filters, arg.TypesToInclude)

	sb = sb.GroupBy("user_email", "server_name", "hook_source", "tool_name").
		OrderBy("event_count DESC").
		Limit(1000) // Defensive cap: top 1000 combinations ordered by volume

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building hooks breakdown query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var breakdown []HooksBreakdownRow
	for rows.Next() {
		var row HooksBreakdownRow
		if err = rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("error scanning hooks breakdown row: %w", err)
		}
		breakdown = append(breakdown, row)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return breakdown, nil
}

// HooksTimeSeriesPoint contains event counts for a single time bucket, server, and user combination.
type HooksTimeSeriesPoint struct {
	BucketStartNs int64  `ch:"bucket_start"`
	ServerName    string `ch:"server_name"`
	UserEmail     string `ch:"user_email"`
	EventCount    uint64 `ch:"event_count"`
	FailureCount  uint64 `ch:"failure_count"`
}

// GetHooksTimeSeriesParams defines the parameters for the hooks time series query.
type GetHooksTimeSeriesParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	BucketSizeNs   int64 // Bucket size in nanoseconds (e.g. 5*60*1e9 for 5 minutes)
	Filters        []AttributeFilter
	TypesToInclude []string
}

// GetHooksTimeSeries retrieves time-bucketed hook event counts grouped by (bucket, server, user).
// BucketSizeNs controls the bucket granularity (e.g. 5 minutes = 5*60*1e9).
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetHooksTimeSeries(ctx context.Context, arg GetHooksTimeSeriesParams) ([]HooksTimeSeriesPoint, error) {
	sb := sq.Select(
		fmt.Sprintf("intDiv(start_time_unix_nano, %d) * %d as bucket_start", arg.BucketSizeNs, arg.BucketSizeNs),
		"if(tool_source = '', 'local', tool_source) as server_name",
		"if(user_email = '', 'Unknown', user_email) as user_email",
		"count(*) as event_count",
		"sumIf(has_error, has_error = 1) as failure_count",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("event_source = 'hook'").
		Where("start_time_unix_nano >= ?", arg.TimeStart).
		Where("start_time_unix_nano <= ?", arg.TimeEnd)

	sb = applyHookFiltersToBuilder(sb, arg.Filters, arg.TypesToInclude)

	sb = sb.GroupBy("bucket_start", "server_name", "user_email").
		OrderBy("bucket_start ASC").
		Limit(10000) // Defensive cap: 288 buckets/day * ~34 server/user combos at 5min resolution

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building hooks time series query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []HooksTimeSeriesPoint
	for rows.Next() {
		var pt HooksTimeSeriesPoint
		if err = rows.ScanStruct(&pt); err != nil {
			return nil, fmt.Errorf("error scanning hooks time series point: %w", err)
		}
		points = append(points, pt)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return points, nil
}

// SkillTimeSeriesPoint contains event counts for a single time bucket and skill combination.
type SkillTimeSeriesPoint struct {
	BucketStartNs int64  `ch:"bucket_start"`
	SkillName     string `ch:"skill_name"`
	EventCount    uint64 `ch:"event_count"`
}

// GetSkillTimeSeriesParams defines the parameters for the skill time series query.
type GetSkillTimeSeriesParams struct {
	GramProjectID string
	TimeStart     int64
	TimeEnd       int64
	BucketSizeNs  int64 // Bucket size in nanoseconds (e.g. 5*60*1e9 for 5 minutes)
	Filters       []AttributeFilter
}

// GetSkillTimeSeries retrieves time-bucketed hook event counts grouped by (bucket, skill).
// BucketSizeNs controls the bucket granularity (e.g. 5 minutes = 5*60*1e9).
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetSkillTimeSeries(ctx context.Context, arg GetSkillTimeSeriesParams) ([]SkillTimeSeriesPoint, error) {
	sb := sq.Select(
		fmt.Sprintf("intDiv(start_time_unix_nano, %d) * %d as bucket_start", arg.BucketSizeNs, arg.BucketSizeNs),
		"skill_name",
		"count(*) as event_count",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("event_source = 'hook'").
		Where("tool_name = 'Skill'").
		Where("start_time_unix_nano >= ?", arg.TimeStart).
		Where("start_time_unix_nano <= ?", arg.TimeEnd).
		Where("skill_name != ''")

	// Apply attribute filters (user, server) but not type filters — skill type is hardcoded above.
	sb = applyHookFiltersToBuilder(sb, arg.Filters, nil)

	sb = sb.GroupBy("bucket_start", "skill_name").
		OrderBy("bucket_start ASC").
		Limit(10000) // Defensive cap

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building skill time series query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []SkillTimeSeriesPoint
	for rows.Next() {
		var pt SkillTimeSeriesPoint
		if err = rows.ScanStruct(&pt); err != nil {
			return nil, fmt.Errorf("scanning skill time series point: %w", err)
		}
		points = append(points, pt)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return points, nil
}

// ListHooksTracesParams contains the parameters for listing hook traces.
type ListHooksTracesParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	Filters        []AttributeFilter
	TypesToInclude []string // Hook types to include: "mcp", "local", "skill"
	SortOrder      string
	Cursor         string // trace_id to paginate from
	Limit          int
}

// ListHooksTraces retrieves aggregated hook trace summaries grouped by trace_id.
// This query directly accesses telemetry_logs to fetch user_email from attributes JSON,
// while using materialized columns for tool_name, tool_source, and event_source.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListHooksTraces(ctx context.Context, arg ListHooksTracesParams) ([]HookTraceSummary, error) {
	sb := sq.Select(
		"trace_id",
		"min(start_time_unix_nano) as start_time_unix_nano",
		"sum(log_count) as log_count",
		"any(gram_urn) as gram_urn",
		"tool_name",
		"tool_source",
		"event_source",
		"user_email",
		"hook_source",
		"skill_name",
		"multiIf(max(has_block) = 1, 'blocked', max(has_error) = 1, 'failure', max(has_result) = 1, 'success', 'pending') as hook_status",
		"max(block_reason) as block_reason",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("event_source = 'hook'").
		Having("start_time_unix_nano >= ?", arg.TimeStart).
		Having("start_time_unix_nano <= ?", arg.TimeEnd).
		Where("trace_id IS NOT NULL AND trace_id != ''")

	// Apply arbitrary attribute filters
	for _, filter := range arg.Filters {
		if !validJSONPath.MatchString(filter.Path) {
			continue // skip invalid paths to prevent injection
		}
		materializedCol, isMaterialized := materializedColumns[filter.Path]
		var columnRef string
		if isMaterialized {
			columnRef = materializedCol
		} else {
			// Not materialized - access via attributes JSON
			columnRef = fmt.Sprintf("toString(attributes.`%s`)", filter.Path)
		}

		switch filter.Op {
		case "eq":
			if len(filter.Values) > 0 {
				sb = sb.Where(squirrel.Eq{columnRef: filter.Values[0]})
			}
		case "not_eq":
			if len(filter.Values) > 0 {
				sb = sb.Where(squirrel.NotEq{columnRef: filter.Values[0]})
			}
		case "contains":
			if len(filter.Values) > 0 {
				sb = sb.Where(fmt.Sprintf("position(%s, ?) > 0", columnRef), filter.Values[0])
			}
		case "in":
			if len(filter.Values) > 0 {
				sb = sb.Where(squirrel.Eq{columnRef: filter.Values})
			}
		case "exists":
			if isMaterialized {
				sb = sb.Where(fmt.Sprintf("%s IS NOT NULL AND %s != ''", columnRef, columnRef))
			} else {
				sb = sb.Where(fmt.Sprintf("has(JSONExtractKeys(attributes), '%s')", filter.Path))
			}
		case "not_exists":
			if isMaterialized {
				sb = sb.Where(squirrel.Or{
					squirrel.Eq{columnRef: nil},
					squirrel.Eq{columnRef: ""},
				})
			} else {
				sb = sb.Where(fmt.Sprintf("NOT has(JSONExtractKeys(attributes), '%s')", filter.Path))
			}
		}
	}

	// Apply hook type filtering if specified
	if len(arg.TypesToInclude) > 0 {
		typeConditions := make([]string, 0, len(arg.TypesToInclude))
		for _, hookType := range arg.TypesToInclude {
			switch hookType {
			case "skill":
				typeConditions = append(typeConditions, "tool_name = 'Skill'")
			case "mcp":
				typeConditions = append(typeConditions, "(tool_source != '' AND tool_name != 'Skill')")
			case "local":
				typeConditions = append(typeConditions, "(tool_source = '' AND tool_name != 'Skill')")
			}
		}
		if len(typeConditions) > 0 {
			sb = sb.Where(fmt.Sprintf("(%s)", strings.Join(typeConditions, " OR ")))
		}
	}

	sb = sb.GroupBy("trace_id", "tool_name", "tool_source", "event_source", "user_email", "hook_source", "skill_name")

	// Pagination based on trace_id cursor
	if arg.Cursor != "" {
		if arg.SortOrder == "asc" {
			sb = sb.Having("start_time_unix_nano > (SELECT min(time_unix_nano) FROM telemetry_logs WHERE gram_project_id = ? AND trace_id = ?)", arg.GramProjectID, arg.Cursor)
		} else {
			sb = sb.Having("start_time_unix_nano < (SELECT min(time_unix_nano) FROM telemetry_logs WHERE gram_project_id = ? AND trace_id = ?)", arg.GramProjectID, arg.Cursor)
		}
	}

	// Apply ordering
	if arg.SortOrder == "asc" {
		sb = sb.OrderBy("start_time_unix_nano ASC")
	} else {
		sb = sb.OrderBy("start_time_unix_nano DESC")
	}

	sb = sb.Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list hooks traces query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var traces []HookTraceSummary
	for rows.Next() {
		var trace HookTraceSummary
		if err = rows.ScanStruct(&trace); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		traces = append(traces, trace)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return traces, nil
}

// TopUser represents a top user by activity.
type TopUser struct {
	UserID        string `ch:"user_id"`
	UserType      string `ch:"user_type"` // "internal" or "external"
	ActivityCount uint64 `ch:"activity_count"`
}

// TopServer represents a top MCP server by tool call count.
type TopServer struct {
	ServerName    string `ch:"server_name"`
	ToolCallCount uint64 `ch:"tool_call_count"`
}

// LLMClientUsage represents usage breakdown by LLM client/agent.
type LLMClientUsage struct {
	ClientName    string `ch:"client_name"`
	ActivityCount uint64 `ch:"activity_count"`
}

// GetTopUsersParams contains parameters for getting top users.
type GetTopUsersParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	ExternalUserID string // Optional filter
	APIKeyID       string // Optional filter
	ToolsetSlug    string // Optional filter
	Limit          int
	SessionMode    bool // If true, count messages; if false, count tool calls
}

// GetTopUsers retrieves top users by activity (messages or tool calls).
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetTopUsers(ctx context.Context, arg GetTopUsersParams) ([]TopUser, error) {
	var activityColumn string
	if arg.SessionMode {
		// Count chat completion messages
		activityColumn = "countIf(toString(attributes.gram.resource.urn) IN ('chat:completion', 'assistants:chat:completion')) as activity_count"
	} else {
		// Count tool calls
		activityColumn = "countIf(startsWith(gram_urn, 'tools:')) as activity_count"
	}

	sb := sq.Select(
		"if(external_user_id != '', external_user_id, user_id) as user_id",
		"if(external_user_id != '', 'external', 'internal') as user_type",
		activityColumn,
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where("if(external_user_id != '', external_user_id, user_id) != ''").
		GroupBy("user_id", "user_type").
		OrderBy("activity_count DESC").
		//nolint:gosec // Limit is bounded by API validation
		Limit(uint64(arg.Limit))

	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}
	if arg.APIKeyID != "" {
		sb = sb.Where(squirrel.Eq{"api_key_id": arg.APIKeyID})
	}
	if arg.ToolsetSlug != "" {
		sb = sb.Where(squirrel.Eq{"toolset_slug": arg.ToolsetSlug})
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building top users query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []TopUser
	for rows.Next() {
		var user TopUser
		if err = rows.ScanStruct(&user); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		users = append(users, user)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// GetTopServersParams contains parameters for getting top servers.
type GetTopServersParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	ExternalUserID string // Optional filter
	APIKeyID       string // Optional filter
	ToolsetSlug    string // Optional filter
	Limit          int
}

// GetTopServers retrieves top MCP servers by tool call count, excluding "local" tool calls.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetTopServers(ctx context.Context, arg GetTopServersParams) ([]TopServer, error) {
	sb := sq.Select(
		"if(tool_source = '', 'local', tool_source) as server_name",
		"count(*) as tool_call_count",
	).
		From("trace_summaries").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("event_source = 'hook'").
		Where("tool_source != ''"). // Exclude "local" tool calls (empty tool_source)
		Where("start_time_unix_nano >= ?", arg.TimeStart).
		Where("start_time_unix_nano <= ?", arg.TimeEnd).
		GroupBy("server_name").
		OrderBy("tool_call_count DESC").
		//nolint:gosec // Limit is bounded by API validation
		Limit(uint64(arg.Limit))

	// Note: trace_summaries doesn't have external_user_id/api_key_id, so we can't filter by those
	// If filtering is needed, we'd have to query telemetry_logs instead

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building top servers query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []TopServer
	for rows.Next() {
		var server TopServer
		if err = rows.ScanStruct(&server); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		servers = append(servers, server)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return servers, nil
}

// GetLLMClientBreakdownParams contains parameters for getting LLM client breakdown.
type GetLLMClientBreakdownParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	ExternalUserID string // Optional filter
	APIKeyID       string // Optional filter
	ToolsetSlug    string // Optional filter
	SessionMode    bool   // If true, count messages; if false, count tool calls
}

// GetLLMClientBreakdown retrieves usage breakdown by LLM client/agent.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetLLMClientBreakdown(ctx context.Context, arg GetLLMClientBreakdownParams) ([]LLMClientUsage, error) {
	var activityColumn string
	if arg.SessionMode {
		// Count chat completion messages
		activityColumn = "countIf(toString(attributes.gram.resource.urn) IN ('chat:completion', 'assistants:chat:completion')) as activity_count"
	} else {
		// Count tool calls
		activityColumn = "countIf(startsWith(gram_urn, 'tools:')) as activity_count"
	}

	sb := sq.Select(
		"toString(attributes.gram.hook.source) as client_name",
		activityColumn,
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where("toString(attributes.gram.hook.source) != ''").
		GroupBy("client_name").
		OrderBy("activity_count DESC")

	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}
	if arg.APIKeyID != "" {
		sb = sb.Where(squirrel.Eq{"api_key_id": arg.APIKeyID})
	}
	if arg.ToolsetSlug != "" {
		sb = sb.Where(squirrel.Eq{"toolset_slug": arg.ToolsetSlug})
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building LLM client breakdown query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []LLMClientUsage
	for rows.Next() {
		var client LLMClientUsage
		if err = rows.ScanStruct(&client); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		clients = append(clients, client)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return clients, nil
}

// GetActiveCountsParams contains parameters for getting active counts.
type GetActiveCountsParams struct {
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	ExternalUserID string // Optional filter
	APIKeyID       string // Optional filter
	ToolsetSlug    string // Optional filter
	SessionMode    bool   // If true, count by messages; if false, count by tool calls
}

// ActiveCounts represents active server and user counts.
type ActiveCounts struct {
	ActiveServersCount uint64 `ch:"active_servers_count"`
	ActiveUsersCount   uint64 `ch:"active_users_count"`
}

// GetActiveCounts retrieves counts of active servers and users.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetActiveCounts(ctx context.Context, arg GetActiveCountsParams) (*ActiveCounts, error) {
	var userCountCondition string
	if arg.SessionMode {
		// Count users with chat completion messages
		userCountCondition = "uniqExactIf(if(external_user_id != '', external_user_id, user_id), toString(attributes.gram.resource.urn) IN ('chat:completion', 'assistants:chat:completion') AND if(external_user_id != '', external_user_id, user_id) != '')"
	} else {
		// Count users with tool calls
		userCountCondition = "uniqExactIf(if(external_user_id != '', external_user_id, user_id), startsWith(gram_urn, 'tools:') AND if(external_user_id != '', external_user_id, user_id) != '')"
	}

	sb := sq.Select(
		"uniqExactIf(tool_source, tool_source != '' AND event_source = 'hook') as active_servers_count",
		userCountCondition+" as active_users_count",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd)

	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}
	if arg.APIKeyID != "" {
		sb = sb.Where(squirrel.Eq{"api_key_id": arg.APIKeyID})
	}
	if arg.ToolsetSlug != "" {
		sb = sb.Where(squirrel.Eq{"toolset_slug": arg.ToolsetSlug})
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building active counts query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return &ActiveCounts{
			ActiveServersCount: 0,
			ActiveUsersCount:   0,
		}, nil
	}

	var counts ActiveCounts
	if err = rows.ScanStruct(&counts); err != nil {
		return nil, fmt.Errorf("error scanning row: %w", err)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return &counts, nil
}

// ListRecentHookEventsForOnboardingParams contains the parameters for the
// onboarding wizard's hook verification query.
type ListRecentHookEventsForOnboardingParams struct {
	// ProjectIDs is the set of Gram project IDs (uuid strings) to query across.
	// Typically every project under the active organization.
	ProjectIDs []string
	// SinceUnixNano returns only events strictly greater than this value.
	// Pass 0 to return the most recent events from any time.
	SinceUnixNano int64
	// Limit caps the number of returned rows.
	Limit int
}

// ListRecentHookEventsForOnboarding returns the most recent hook events for the
// given project IDs, newest first. It powers the onboarding wizard's
// confirm-traffic step, which polls this endpoint to verify that Claude Code /
// Cursor / Codex hooks are flowing into Gram after the user finishes
// instrumentation.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListRecentHookEventsForOnboarding(ctx context.Context, arg ListRecentHookEventsForOnboardingParams) ([]RecentHookEvent, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}

	sb := sq.Select(
		"time_unix_nano",
		"gram_project_id",
		"gram_chat_id",
		"hook_source",
		"tool_name",
		"user_email",
		"toString(attributes.gram.hook.event) AS event_name",
		"multiIf(hook_block_reason IS NOT NULL AND hook_block_reason != '', 'blocked', 'received') AS status",
	).
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("event_source = 'hook'").
		Where("toString(attributes.gram.hook.event) != ''")

	if arg.SinceUnixNano > 0 {
		sb = sb.Where("time_unix_nano > ?", arg.SinceUnixNano)
	}

	sb = sb.OrderBy("time_unix_nano DESC").
		Limit(uint64(arg.Limit)) //nolint:gosec // Limit is always positive

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list recent hook events query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []RecentHookEvent
	for rows.Next() {
		var ev RecentHookEvent
		if err = rows.ScanStruct(&ev); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		events = append(events, ev)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

type ListClaudeUserPromptCandidatesForCorrelationParams struct {
	GramProjectID          string
	GramChatID             string
	SessionID              string
	MessagePrompt          string
	MessageTimeUnixNano    int64
	AfterEventSequence     int64
	AfterEventTimeUnixNano int64
	MinFuzzyLength         int
	MaxTimeDeltaNanos      int64
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListClaudeUserPromptCandidatesForCorrelation(ctx context.Context, arg ListClaudeUserPromptCandidatesForCorrelationParams) ([]ClaudeUserPromptCandidate, error) {
	if arg.MessagePrompt == "" {
		return nil, nil
	}
	if len(arg.MessagePrompt) > MaxClaudePromptCorrelationEditDistanceBytes {
		return nil, nil
	}

	ctx = clickhouse.Context(ctx, clickhouse.WithParameters(clickhouse.Parameters{
		"gram_project_id":               arg.GramProjectID,
		"gram_chat_id":                  arg.GramChatID,
		"session_id":                    arg.SessionID,
		"message_prompt_b64":            base64.StdEncoding.EncodeToString([]byte(arg.MessagePrompt)),
		"max_edit_distance_bytes":       strconv.Itoa(MaxClaudePromptCorrelationEditDistanceBytes),
		"message_time_unix_nano":        strconv.FormatInt(arg.MessageTimeUnixNano, 10),
		"after_event_sequence":          strconv.FormatInt(arg.AfterEventSequence, 10),
		"after_event_time_unix_nano":    strconv.FormatInt(arg.AfterEventTimeUnixNano, 10),
		"min_fuzzy_length":              strconv.Itoa(arg.MinFuzzyLength),
		"max_time_delta_nanos":          strconv.FormatInt(arg.MaxTimeDeltaNanos, 10),
		"negative_max_time_delta_nanos": strconv.FormatInt(-arg.MaxTimeDeltaNanos, 10),
	}))

	normalizedPromptExpr := "replaceRegexpAll(trimBoth(toString(attributes.prompt)), '\\\\s+', ' ')"
	rawEvents := sq.Select(
		"toString(attributes.prompt.id) AS prompt_id",
		normalizedPromptExpr+" AS prompt",
		"toInt64OrZero(toString(attributes.event.sequence)) AS event_sequence",
		"time_unix_nano",
	).
		From("telemetry_logs").
		Where("gram_project_id = {gram_project_id:String}").
		Where("gram_chat_id = {gram_chat_id:String}").
		Where("toString(attributes.session.id) = {session_id:String}").
		Where("toString(attributes.event.name) = 'user_prompt'").
		Where("toString(attributes.prompt.id) != ''").
		Where("toString(attributes.prompt) != ''").
		Where("length(" + normalizedPromptExpr + ") <= {max_edit_distance_bytes:UInt64}").
		Where(squirrel.Or{
			squirrel.Expr("event_sequence > {after_event_sequence:Int64}"),
			squirrel.Expr(`(
				event_sequence = {after_event_sequence:Int64}
				AND time_unix_nano > {after_event_time_unix_nano:Int64}
			)`),
		}).
		Where(`time_unix_nano BETWEEN message_time_unix_nano + {negative_max_time_delta_nanos:Int64}
			AND message_time_unix_nano + max_time_delta_nanos`)

	scoredCandidates := sq.Select(
		"prompt_id",
		"prompt",
		"event_sequence",
		"time_unix_nano",
		"prompt = message_prompt AS is_exact",
		`if(
			prompt = message_prompt,
			1.0,
			1.0 - (
				toFloat64(editDistanceUTF8(prompt, message_prompt))
				/ toFloat64(greatest(lengthUTF8(prompt), message_len, 1))
			)
		) AS similarity,
		abs(time_unix_nano - message_time_unix_nano) AS time_delta`,
	).
		FromSelect(rawEvents, "raw_events")

	sb := sq.Select(
		"prompt_id",
		"prompt",
		"event_sequence",
		"time_unix_nano",
		"similarity",
		"is_exact",
	).
		Prefix(`WITH
			replaceRegexpAll(trimBoth(base64Decode({message_prompt_b64:String})), '\\s+', ' ') AS message_prompt,
			lengthUTF8(message_prompt) AS message_len,
			{message_time_unix_nano:Int64} AS message_time_unix_nano,
			{max_time_delta_nanos:Int64} AS max_time_delta_nanos`).
		FromSelect(scoredCandidates, "scored_candidates").
		Where(squirrel.Or{
			squirrel.Expr("is_exact"),
			squirrel.And{
				squirrel.Expr("message_len >= {min_fuzzy_length:UInt64}"),
				squirrel.Expr("lengthUTF8(prompt) >= {min_fuzzy_length:UInt64}"),
				squirrel.Expr("time_delta <= max_time_delta_nanos"),
			},
		}).
		OrderBy("is_exact DESC", "similarity DESC", "time_delta ASC", "event_sequence ASC", "time_unix_nano ASC").
		Limit(2)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list Claude user prompt candidates query: %w", err)
	}
	if len(args) > 0 {
		return nil, fmt.Errorf("building list Claude user prompt candidates query: unexpected positional arguments")
	}

	rows, err := q.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []ClaudeUserPromptCandidate
	for rows.Next() {
		var event ClaudeUserPromptCandidate
		if err = rows.ScanStruct(&event); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

// CountRecentHookEventsForOnboarding returns the total number of hook events
// for the given projects with time_unix_nano > since_unix_nano. Used alongside
// ListRecentHookEventsForOnboarding to report a total when the list is
// truncated by Limit.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) CountRecentHookEventsForOnboarding(ctx context.Context, projectIDs []string, sinceUnixNano int64) (uint64, error) {
	if len(projectIDs) == 0 {
		return 0, nil
	}

	sb := sq.Select("count() AS cnt").
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": projectIDs}).
		Where("event_source = 'hook'").
		Where("toString(attributes.gram.hook.event) != ''")

	if sinceUnixNano > 0 {
		sb = sb.Where("time_unix_nano > ?", sinceUnixNano)
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return 0, fmt.Errorf("building count recent hook events query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var cnt uint64
	if rows.Next() {
		if err := rows.Scan(&cnt); err != nil {
			return 0, fmt.Errorf("scanning count row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return cnt, nil
}

// GetTokensUnderManagementParams contains the parameters for computing tokens
// under management for a set of projects over a time window.
type GetTokensUnderManagementParams struct {
	ProjectIDs    []string
	StartUnixNano int64
	EndUnixNano   int64
	// BilledHookSources restricts the token sums to chats consumed through
	// these surfaces (billing.ModelUsageSources). Rows aggregated before the
	// hook_source dimension existed carry '' and are grandfathered as billed
	// — sealed cycles beyond the migration's rewrite window keep their
	// invoiced totals. Empty means no source scoping.
	BilledHookSources []string
}

// billedHookSourceFilter scopes a chat_token_summaries read to the billed
// completion surfaces, grandfathering pre-dimension rows (”).
func billedHookSourceFilter(sb squirrel.SelectBuilder, sources []string) squirrel.SelectBuilder {
	if len(sources) == 0 {
		return sb
	}
	return sb.Where(squirrel.Or{
		squirrel.Eq{"hook_source": sources},
		squirrel.Eq{"hook_source": ""},
	})
}

// TumDayBucket is one UTC day's worth of tokens under management.
type TumDayBucket struct {
	Day    time.Time `ch:"time_bucket"`
	Tokens int64     `ch:"tokens"`
}

// GetTokensUnderManagementByDay sums token usage per UTC day for the billing
// window, counting only sessions Gram has stored non-metrics data for (chats,
// tool calls). OTEL forwarding can report token usage for an entire customer
// org while Gram is installed for a subset of users, so a chat's tokens only
// count when at least one non-metrics row (a tool call, a hook event, or any
// row without a token-usage attribute) was recorded for it inside the window.
//
// Reads the chat_token_summaries aggregate, which buckets by day and is
// retained well beyond the raw telemetry TTL, so historical billing cycles
// stay computable. Window boundaries are day-granular: the start rounds down
// to its UTC day and the end is expected to be a UTC day boundary, which
// billing cycle boundaries always are. Days without usage are omitted.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetTokensUnderManagementByDay(ctx context.Context, arg GetTokensUnderManagementParams) ([]TumDayBucket, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}

	storedChats := sq.Select("DISTINCT chat_id").
		From("chat_token_summaries").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("time_bucket >= toStartOfDay(fromUnixTimestamp64Nano(?))", arg.StartUnixNano).
		Where("time_bucket < fromUnixTimestamp64Nano(?)", arg.EndUnixNano).
		Where("chat_id != ''").
		Where("stored_event_count > 0")

	storedChatsSQL, storedChatsArgs, err := storedChats.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tum stored chats subquery: %w", err)
	}

	sb := sq.Select(
		"time_bucket",
		"sum(total_tokens) AS tokens",
	).
		From("chat_token_summaries").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("time_bucket >= toStartOfDay(fromUnixTimestamp64Nano(?))", arg.StartUnixNano).
		Where("time_bucket < fromUnixTimestamp64Nano(?)", arg.EndUnixNano).
		Where("chat_id != ''").
		Where(squirrel.Expr("chat_id IN ("+storedChatsSQL+")", storedChatsArgs...)).
		GroupBy("time_bucket").
		OrderBy("time_bucket")
	// The token sum is source-scoped; the stored-chats qualification above is
	// not — stored evidence is chat-level, whatever surface recorded it.
	sb = billedHookSourceFilter(sb, arg.BilledHookSources)

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tokens under management by day query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []TumDayBucket
	for rows.Next() {
		var bucket TumDayBucket
		if err := rows.ScanStruct(&bucket); err != nil {
			return nil, fmt.Errorf("scanning tokens under management day row: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return buckets, nil
}

// billedStoredChatsSubquery builds the stored-session qualification subquery
// on chat_token_summaries — the same rule the billed totals apply, so every
// dimensioned read below describes exactly the billed population.
func billedStoredChatsSubquery(arg GetTokensUnderManagementParams) (string, []any, error) {
	sb := sq.Select("DISTINCT chat_id").
		From("chat_token_summaries").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("time_bucket >= toStartOfDay(fromUnixTimestamp64Nano(?))", arg.StartUnixNano).
		Where("time_bucket < fromUnixTimestamp64Nano(?)", arg.EndUnixNano).
		Where("chat_id != ''").
		Where("stored_event_count > 0")
	sql, args, err := sb.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building tum stored chats subquery: %w", err)
	}
	return sql, args, nil
}

// tumBreakdownBase applies the shared window, qualification, and source
// scoping for reads over tum_breakdown_summaries.
func tumBreakdownBase(sb squirrel.SelectBuilder, arg GetTokensUnderManagementParams) (squirrel.SelectBuilder, error) {
	storedChatsSQL, storedChatsArgs, err := billedStoredChatsSubquery(arg)
	if err != nil {
		return sb, err
	}
	sb = sb.
		From("tum_breakdown_summaries").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("time_bucket >= toStartOfDay(fromUnixTimestamp64Nano(?))", arg.StartUnixNano).
		Where("time_bucket < fromUnixTimestamp64Nano(?)", arg.EndUnixNano).
		Where(squirrel.Expr("chat_id IN ("+storedChatsSQL+")", storedChatsArgs...))
	return billedHookSourceFilter(sb, arg.BilledHookSources), nil
}

// TumBreakdownDayBucket is one UTC day of billed tokens split
// by type.
type TumBreakdownDayBucket struct {
	Day          time.Time `ch:"time_bucket"`
	InputTokens  int64     `ch:"input_tokens"`
	OutputTokens int64     `ch:"output_tokens"`
	TotalTokens  int64     `ch:"sum_total_tokens"`
}

// GetTumBreakdownTotalsByDay sums the billed completion token split per
// UTC day, qualified and source-scoped identically to the billed totals.
// Days without usage are omitted (callers gap-fill).
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetTumBreakdownTotalsByDay(ctx context.Context, arg GetTokensUnderManagementParams) ([]TumBreakdownDayBucket, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}

	// The total alias must NOT be "total_tokens": ClickHouse lets a SELECT
	// alias shadow the source column (ILLEGAL_AGGREGATION).
	sb, err := tumBreakdownBase(sq.Select(
		"time_bucket",
		"sum(input_tokens) AS input_tokens",
		"sum(output_tokens) AS output_tokens",
		"sum(total_tokens) AS sum_total_tokens",
	), arg)
	if err != nil {
		return nil, err
	}
	sb = sb.GroupBy("time_bucket").OrderBy("time_bucket")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tum breakdown totals query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []TumBreakdownDayBucket
	for rows.Next() {
		var bucket TumBreakdownDayBucket
		if err := rows.ScanStruct(&bucket); err != nil {
			return nil, fmt.Errorf("scanning tum breakdown totals row: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return buckets, nil
}

// riskAnalysisRowPredicate classifies a billed row as the platform's own
// risk-policy analysis inference — the metered unit of the enterprise TUM
// contracts. Declared rows carry the dedicated source
// (billing.ModelUsageSourceRiskAnalysis); the second clause grandfathers
// rows emitted before that source existed, fingerprinted as internal
// inference: gram/” source with the nil chat id (background workers run
// completions outside any stored chat). Other internal nil-chat inference
// (title generation, chat resolutions, memory) rides along under the same
// clause — a deliberate simplification, it is a rounding error next to the
// judges and is platform-side analysis either way.
const riskAnalysisRowPredicate = "(hook_source = 'risk-analysis' OR (hook_source IN ('gram', '') AND chat_id = '00000000-0000-0000-0000-000000000000'))"

// tumBreakdownDim describes one billing-page breakdown dimension: the
// grouping expression over tum_breakdown_summaries, plus an optional row
// filter for dimensions that slice the billed population (the two model
// sections) rather than partition it by a column.
type tumBreakdownDim struct {
	expr   string
	filter string
}

// tumBreakdownDimExprs maps the billing page's breakdown dimensions to
// their tum_breakdown_summaries expressions. Roles are multi-valued: a
// session's tokens count once under each held role, so role rows overlap.
// The model dimension is split in two: risk_analysis_model covers the
// platform's scanning inference and completion_model covers user-facing
// completion surfaces — together they partition the billed population.
var tumBreakdownDimExprs = map[string]tumBreakdownDim{
	"hook_source":         {expr: "hook_source", filter: ""},
	"risk_analysis_model": {expr: "model", filter: riskAnalysisRowPredicate},
	"completion_model":    {expr: "model", filter: "NOT " + riskAnalysisRowPredicate},
	// email is plumbed but NOT in the service's tumBreakdownDims: a per-user
	// cut of billed usage (which now includes scanned-user attribution of
	// risk-analysis inference) is deliberately not exposed on the billing
	// page yet.
	"email":         {expr: "user_email", filter: ""},
	"division_name": {expr: "division_name", filter: ""},
	"role":          {expr: "arrayJoin(roles)", filter: ""},
}

// TumBreakdownDimDayBucket is one (UTC day, dimension value) slice of
// billed tokens.
type TumBreakdownDimDayBucket struct {
	Day    time.Time `ch:"time_bucket"`
	Value  string    `ch:"dim_value"`
	Tokens int64     `ch:"tokens"`
}

// GetTumBreakdownDimByDay returns the billed daily token series per value
// of one breakdown dimension, qualified and source-scoped identically to the
// billed totals so the slices sum to them exactly (except the multi-valued
// role dimension, whose rows overlap).
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetTumBreakdownDimByDay(ctx context.Context, arg GetTokensUnderManagementParams, dimension string) ([]TumBreakdownDimDayBucket, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}
	dim, ok := tumBreakdownDimExprs[dimension]
	if !ok {
		return nil, fmt.Errorf("unsupported tum breakdown dimension: %q", dimension)
	}

	sb, err := tumBreakdownBase(sq.Select(
		"time_bucket",
		dim.expr+" AS dim_value",
		"sum(total_tokens) AS tokens",
	), arg)
	if err != nil {
		return nil, err
	}
	if dim.filter != "" {
		sb = sb.Where(dim.filter)
	}
	sb = sb.GroupBy("time_bucket", "dim_value").OrderBy("time_bucket", "dim_value")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building tum breakdown dimension query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []TumBreakdownDimDayBucket
	for rows.Next() {
		var bucket TumBreakdownDimDayBucket
		if err := rows.ScanStruct(&bucket); err != nil {
			return nil, fmt.Errorf("scanning tum breakdown dimension row: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return buckets, nil
}

// GetRiskTokensParams contains the parameters for the per-day token split by
// risk involvement. RiskyChatIDs is the set of chats (resolved from Postgres
// risk findings) whose tokens count as risky.
type GetRiskTokensParams struct {
	ProjectIDs    []string
	RiskyChatIDs  []string
	StartUnixNano int64
	EndUnixNano   int64
	// HookSources restricts the risk-token reads to chats from these sources
	// (billing.ModelUsageSources), grandfathering '' rows aggregated before
	// chat_token_summaries had a hook_source dimension — matching
	// GetTokensUnderManagementByDay so the risk split and the billed totals
	// describe the same population. Empty means no source scoping.
	HookSources []string
}

// RiskTokensDayBucket is one UTC day of token usage split into risky vs total.
type RiskTokensDayBucket struct {
	Day         time.Time `ch:"time_bucket"`
	TotalTokens int64     `ch:"tokens"`
	RiskyTokens int64     `ch:"risky_tokens"`
}

// GetRiskTokensByDay sums token usage per UTC day, alongside the subset from
// chats in RiskyChatIDs. Reads the chat_token_summaries daily aggregate — the
// same source as tokens under management — but without the stored-session
// qualification, so the totals line up with the costs page's token charts.
// Days without usage are omitted (callers gap-fill).
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetRiskTokensByDay(ctx context.Context, arg GetRiskTokensParams) ([]RiskTokensDayBucket, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}

	// The total alias must NOT be "total_tokens": ClickHouse lets a SELECT
	// alias shadow the source column, which would turn the sumIf's column
	// reference into a nested aggregate (ILLEGAL_AGGREGATION).
	sb := sq.Select(
		"time_bucket",
		"sum(total_tokens) AS tokens",
	).
		From("chat_token_summaries").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("time_bucket >= toStartOfDay(fromUnixTimestamp64Nano(?))", arg.StartUnixNano).
		Where("time_bucket < fromUnixTimestamp64Nano(?)", arg.EndUnixNano).
		Where("chat_id != ''").
		GroupBy("time_bucket").
		OrderBy("time_bucket")
	sb = billedHookSourceFilter(sb, arg.HookSources)

	// clickhouse-go expands a Go slice bound to a single placeholder into a
	// comma-joined value list, which only parses inside IN (...) — so the risky
	// set rides in one parameter there. An empty set short-circuits to a
	// constant zero instead of binding an empty list (invalid SQL).
	if len(arg.RiskyChatIDs) > 0 {
		sb = sb.Column(squirrel.Expr("sumIf(total_tokens, chat_id IN (?)) AS risky_tokens", arg.RiskyChatIDs))
	} else {
		sb = sb.Column("toInt64(0) AS risky_tokens")
	}

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building risk tokens by day query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []RiskTokensDayBucket
	for rows.Next() {
		var bucket RiskTokensDayBucket
		if err := rows.ScanStruct(&bucket); err != nil {
			return nil, fmt.Errorf("scanning risk tokens day row: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return buckets, nil
}
