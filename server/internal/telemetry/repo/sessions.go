package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Masterminds/squirrel"
)

const (
	// sessionClaudeOTELRowPredicate anchors Claude provenance on the OTEL log
	// stream URN stamped at ingest. Claude usage and tool calls are derived
	// exclusively from these rows; Claude hook rows and claude-code:usage
	// metric rows are never sources. Mirrors is_claude_otel_row in
	// attribute_metrics_summaries_mv (server/clickhouse/schema.sql) — keep the
	// session* predicates in sync with the MV's WITH clause.
	sessionClaudeOTELRowPredicate = "(gram_urn = 'claude-code:otel:logs')"
	// sessionClaudeAPIRequestPredicate matches Claude Code api_request rows — the
	// authoritative source of Claude token/cost and MCP/skill/agent attribution.
	sessionClaudeAPIRequestPredicate = "(" +
		sessionClaudeOTELRowPredicate + " AND " +
		"chat_id != '' AND " +
		"toString(attributes.prompt.id) != '' AND " +
		"(toString(attributes.event.name) = 'api_request' OR body = 'claude_code.api_request')" +
		")"
	// sessionClaudeToolResultPredicate matches Claude tool_result rows — one per
	// completed tool call, the sole Claude tool-call source.
	sessionClaudeToolResultPredicate = "(" +
		sessionClaudeOTELRowPredicate + " AND " +
		"(toString(attributes.event.name) = 'tool_result' OR body = 'claude_code.tool_result')" +
		")"
	// sessionAgentUsageRowPredicate matches Codex/Cursor/Claude-Chat usage
	// rows — their only token/cost source. claude_chat:usage rows carry Claude
	// Chat (web/desktop) token usage and claude_chat:cost rows the matching
	// spend, both polled from the Admin Analytics API. Gram-hosted chat
	// completions and claude-code:usage rows are deliberately excluded: the
	// summaries cover agent surfaces only, and claude-code:usage duplicates
	// the OTEL api_request stream.
	sessionAgentUsageRowPredicate = "(startsWith(gram_urn, 'codex:usage') OR startsWith(gram_urn, 'cursor:usage') OR startsWith(gram_urn, 'claude_chat:usage') OR startsWith(gram_urn, 'claude_chat:cost'))"
	// sessionAgentToolCallPredicate matches Codex/Cursor completed tool-call hook
	// rows (they have no OTEL stream). The hook.event guard excludes the
	// PreToolUse companion row; provider names are not tool calls.
	sessionAgentToolCallPredicate = "(" +
		"hook_source IN ('codex', 'cursor') AND " +
		"toString(attributes.gram.tool.name) != '' AND " +
		"toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor') AND " +
		"toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')" +
		")"
	sessionCountedToolCallPredicate = "(" + sessionClaudeToolResultPredicate + " OR " + sessionAgentToolCallPredicate + ")"
	// sessionFailedToolCallPredicate marks a counted tool call as failed: Claude
	// tool_result rows carry success="false", Codex/Cursor hook rows report
	// PostToolUseFailure or an HTTP error status.
	sessionFailedToolCallPredicate = "(" +
		"(" + sessionClaudeToolResultPredicate + " AND toString(attributes.success) = 'false') OR " +
		"(" + sessionAgentToolCallPredicate + " AND " +
		"(toString(attributes.gram.hook.event) = 'PostToolUseFailure' OR toInt32OrZero(toString(attributes.http.response.status_code)) >= 400))" +
		")"
	// sessionToolCallDedupIDExpr is the call's identity for deduplicated
	// counting: Claude tool_result rows carry tool_use_id, Cursor/unified-ingest
	// hook rows carry gen_ai.tool.call.id, and rows with no call id fall back to
	// the row id (count-per-row). Mirrors tool_call_dedup_id in the MV.
	sessionToolCallDedupIDExpr = "multiIf(" +
		"toString(attributes.tool_use_id) != '', toString(attributes.tool_use_id), " +
		"toString(attributes.gen_ai.tool.call.id) != '', toString(attributes.gen_ai.tool.call.id), " +
		"toString(id))"
	// sessionUsageMeasureFilter selects the rows that carry token/cost usage:
	// Claude api_request rows and Codex/Cursor usage rows. This is the sumIf
	// guard for every token/cost measure, keeping session totals aligned with
	// the aggregate.
	sessionUsageMeasureFilter = "(" + sessionClaudeAPIRequestPredicate + " OR " + sessionAgentUsageRowPredicate + ")"
	// sessionSourceRowPredicate admits every row class the session list derives
	// from, matching the aggregate MV's WHERE clause so the two views cover the
	// same sessions.
	sessionSourceRowPredicate = "(" + sessionClaudeAPIRequestPredicate + " OR " + sessionClaudeToolResultPredicate + " OR " + sessionAgentUsageRowPredicate + " OR " + sessionAgentToolCallPredicate + ")"

	// Token/cost measures are source-aware: Claude api_request rows carry usage on
	// flat attributes (input_tokens, cost_usd, …), while generic usage rows carry
	// it under gen_ai.usage.*. These mirror attribute_metrics_summaries_mv exactly.
	sessionInputTokensExpr = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"toInt64OrZero(toString(attributes.input_tokens)), " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))), " + sessionUsageMeasureFilter + ")"
	sessionOutputTokensExpr = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"toInt64OrZero(toString(attributes.output_tokens)), " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))), " + sessionUsageMeasureFilter + ")"
	// total_tokens is input + output + cache WRITES — cache reads are excluded,
	// matching the aggregate MV and the tokens-under-management measure. Both
	// branches sum the disjoint components rather than trusting a reported
	// total (Codex's gen_ai.usage.total_tokens includes cache reads; Cursor
	// usage rows report no total at all).
	sessionTotalTokensExpr = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"toInt64OrZero(toString(attributes.input_tokens)) + toInt64OrZero(toString(attributes.output_tokens)) + " +
		"toInt64OrZero(toString(attributes.cache_creation_tokens)), " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)) + toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)) + " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens))), " + sessionUsageMeasureFilter + ")"
	sessionCacheReadTokensExpr = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"toInt64OrZero(toString(attributes.cache_read_tokens)), " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens))), " + sessionUsageMeasureFilter + ")"
	sessionCacheCreationTokensExpr = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"toInt64OrZero(toString(attributes.cache_creation_tokens)), " +
		"toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens))), " + sessionUsageMeasureFilter + ")"
	sessionCostExpr = "sumIf(if(" + sessionClaudeAPIRequestPredicate + ", " +
		"multiIf(toString(attributes.cost_usd) != '', toFloat64OrZero(toString(attributes.cost_usd)), " +
		"toString(attributes.cost_usd_micros) != '', toFloat64OrZero(toString(attributes.cost_usd_micros)) / 1000000, 0), " +
		"toFloat64OrZero(toString(attributes.gen_ai.usage.cost))), " + sessionUsageMeasureFilter + ")"

	// sessionModelExpr is the per-row effective model. Claude api_request rows put
	// it on attributes.model / attributes.gen_ai.request.model; everyone else on
	// gen_ai.response.model. Mirrors the aggregate MV's model expression so the
	// Model dimension resolves for Claude sessions too. Shared with the model
	// filter in dimensions.go.
	sessionModelExpr = "multiIf(" +
		sessionClaudeAPIRequestPredicate + " AND toString(attributes.model) != '', toString(attributes.model), " +
		sessionClaudeAPIRequestPredicate + " AND toString(attributes.gen_ai.request.model) != '', toString(attributes.gen_ai.request.model), " +
		"toString(attributes.gen_ai.response.model))"

	// sessionMessageIDExpr identifies a distinct message/turn per row: Claude
	// api_request rows are one turn each (unique prompt.id); generic rows key off
	// gen_ai.response.id. Counted distinct for message_count.
	sessionMessageIDExpr = "if(" + sessionClaudeAPIRequestPredicate + ", " +
		"toString(attributes.prompt.id), toString(attributes.gen_ai.response.id))"
	sessionMessageCountExpr = "uniqExactIf(" + sessionMessageIDExpr + ", " + sessionMessageIDExpr + " != '')"
)

// #nosec G101 -- These are allowlisted SQL measure expressions, not credentials.
var sessionMeasureSelects = map[string]string{
	"total_cost":                  sessionCostExpr,
	"total_input_tokens":          sessionInputTokensExpr,
	"total_output_tokens":         sessionOutputTokensExpr,
	"total_tokens":                sessionTotalTokensExpr,
	"cache_read_input_tokens":     sessionCacheReadTokensExpr,
	"cache_creation_input_tokens": sessionCacheCreationTokensExpr,
	"tool_call_count":             "uniqExactIf(" + sessionToolCallDedupIDExpr + ", " + sessionCountedToolCallPredicate + ")",
	"message_count":               sessionMessageCountExpr,
	"duration_seconds":            "toFloat64(max(time_unix_nano) - min(time_unix_nano)) / 1000000000.0",
	// Kept as a service-level compatibility alias; the public listSessions API
	// uses tool_call_count.
	"total_tool_calls": "uniqExactIf(" + sessionToolCallDedupIDExpr + ", " + sessionCountedToolCallPredicate + ")",
}

type ListSessionsParams struct {
	ProjectIDs       []string
	TimeStart        int64
	TimeEnd          int64
	Filters          []AttributeMetricsFilter
	SortBy           string
	CursorSortValue  *float64
	CursorGramChatID string
	Limit            int
}

type SessionSummary struct {
	GramChatID        string  `ch:"gram_chat_id"`
	ProjectID         string  `ch:"project_id"`
	UserEmail         *string `ch:"session_user_email"`
	HookSource        *string `ch:"session_hook_source"`
	Model             *string `ch:"session_model"`
	StartTimeUnixNano int64   `ch:"start_time_unix_nano"`
	EndTimeUnixNano   int64   `ch:"end_time_unix_nano"`
	DurationSeconds   float64 `ch:"duration_seconds"`
	MessageCount      int64   `ch:"message_count"`
	ToolCallCount     int64   `ch:"tool_call_count"`
	TotalInputTokens  int64   `ch:"total_input_tokens"`
	TotalOutputTokens int64   `ch:"total_output_tokens"`
	TotalTokens       int64   `ch:"total_tokens"`
	TotalCost         float64 `ch:"total_cost"`
	Status            string  `ch:"status"`
	SortValue         float64 `ch:"sort_value"`
}

// applySessionFilters restricts the session aggregation to chats matching the
// requested dimension filters. project_id stays a row-level WHERE because it is
// present on every row and prunes partitions. Identity dimensions are matched
// per-chat via HAVING: a chat qualifies when ANY of its rows carries the
// requested value. This is required because those attributes can be stamped on
// different physical rows within the same chat.
//
// Claude attribution dimensions are different: the aggregate summary treats
// query_source/skill/agent/MCP values as a single api_request-row tuple. Keep
// those filters co-located inside one countIf so drilling from the aggregate
// table finds chats that have a row matching the same tuple.
func applySessionFilters(sb squirrel.SelectBuilder, filters []AttributeMetricsFilter) (squirrel.SelectBuilder, error) {
	var coLocatedPredicates []squirrel.Sqlizer

	for _, f := range filters {
		if len(f.Values) == 0 {
			continue
		}
		dim, ok := sessionDimensionRegistry[f.Dimension]
		if !ok {
			return sb, fmt.Errorf("unknown filter dimension %q", f.Dimension)
		}
		switch dim.kind {
		case attributeDimProject:
			sb = sb.Where(squirrel.Eq{dim.column: f.Values})
		case attributeDimScalar:
			if dim.coLocateSessionFilters {
				coLocatedPredicates = append(coLocatedPredicates, sessionScalarRowPredicate(dim.column, f.Values))
				continue
			}
			sb = sb.Having(sessionScalarHaving(dim.column, f.Values))
		case attributeDimArray:
			inner, args, err := arrayDimFilter(dim.column, f.Values).ToSql()
			if err != nil {
				return sb, fmt.Errorf("building array filter for %q: %w", f.Dimension, err)
			}
			sb = sb.Having(squirrel.Expr("countIf("+inner+") > 0", args...))
		default:
			return sb, fmt.Errorf("unhandled dimension kind for filter %q", f.Dimension)
		}
	}
	if len(coLocatedPredicates) > 0 {
		inner, args, err := squirrel.And(coLocatedPredicates).ToSql()
		if err != nil {
			return sb, fmt.Errorf("building co-located session filters: %w", err)
		}
		sb = sb.Having(squirrel.Expr("countIf("+inner+") > 0", args...))
	}
	return sb, nil
}

// sessionScalarRowPredicate matches a single telemetry row against one scalar
// dimension filter. Unlike sessionScalarHaving, a requested "" means "this row
// has an empty value", not "the whole chat has no value anywhere".
func sessionScalarRowPredicate(expr string, values []string) squirrel.Sqlizer {
	hasEmpty := false
	nonEmpty := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			hasEmpty = true
			continue
		}
		nonEmpty = append(nonEmpty, v)
	}

	emptyPred := squirrel.Expr(expr + " = ''")
	if len(nonEmpty) == 0 {
		return emptyPred
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(nonEmpty)), ",")
	args := make([]any, len(nonEmpty))
	for i, v := range nonEmpty {
		args[i] = v
	}
	nonEmptyPred := squirrel.Expr(expr+" IN ("+placeholders+")", args...)
	if !hasEmpty {
		return nonEmptyPred
	}
	return squirrel.Or{nonEmptyPred, emptyPred}
}

// sessionScalarHaving matches a chat when any of its rows carries one of the
// requested scalar values. A requested "" (the "(unset)" bucket) matches chats
// that have no non-empty value for the dimension on any row, mirroring how the
// session's effective value is computed with anyIf over non-empty values.
func sessionScalarHaving(expr string, values []string) squirrel.Sqlizer {
	hasEmpty := false
	nonEmpty := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			hasEmpty = true
			continue
		}
		nonEmpty = append(nonEmpty, v)
	}

	emptyPred := squirrel.Expr("countIf(" + expr + " != '') = 0")
	if len(nonEmpty) == 0 {
		return emptyPred
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(nonEmpty)), ",")
	args := make([]any, len(nonEmpty))
	for i, v := range nonEmpty {
		args[i] = v
	}
	nonEmptyPred := squirrel.Expr("countIf("+expr+" IN ("+placeholders+")) > 0", args...)
	if !hasEmpty {
		return nonEmptyPred
	}
	return squirrel.Or{nonEmptyPred, emptyPred}
}

// sessionSummaryMinWindow routes ListSessions between the raw telemetry_logs
// scan and the pre-aggregated chat_session_summaries read (INC-417). The
// summary is hour-bucketed, so its window edges snap to bucket boundaries —
// up to ~1h of slop, which is noise on multi-day windows but unacceptable on
// the sub-day presets (15m/1h/4h). Short windows are also exactly where the
// raw scan is already cheap (the primary key prunes it to the window's
// granules), so they stay on the raw path.
const sessionSummaryMinWindow = 48 * time.Hour

// ListSessions retrieves org-scoped session summaries grouped by chat_id from
// the same source-event classes as attribute_metrics_summaries: Claude OTEL
// api_request/tool_result rows and Codex/Cursor usage plus tool-call hook
// rows. Pagination is based on the selected sort measure plus chat_id so
// ordering stays stable across pages.
//
// Wide windows read the pre-aggregated chat_session_summaries table; narrow
// windows scan raw telemetry_logs (see sessionSummaryMinWindow).
func (q *Queries) ListSessions(ctx context.Context, arg ListSessionsParams) ([]SessionSummary, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}
	if arg.TimeEnd-arg.TimeStart >= sessionSummaryMinWindow.Nanoseconds() {
		return q.listSessionsFromSummaries(ctx, arg)
	}
	return q.listSessionsFromRawLogs(ctx, arg)
}

// listSessionsFromRawLogs derives session summaries by scanning raw
// telemetry_logs — exact to the nanosecond, but per-row JSON extraction makes
// it expensive on wide windows.
//
//nolint:wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) listSessionsFromRawLogs(ctx context.Context, arg ListSessionsParams) ([]SessionSummary, error) {
	sortExpr, ok := sessionMeasureSelects[arg.SortBy]
	if !ok {
		return nil, fmt.Errorf("unknown sort_by measure %q", arg.SortBy)
	}

	sb := sq.Select(
		"chat_id as gram_chat_id",
		"any(toString(gram_project_id)) as project_id",
		"anyIf(user_email, user_email != '') as session_user_email",
		"anyIf(hook_source, hook_source != '') as session_hook_source",
		"argMaxIf("+sessionModelExpr+", time_unix_nano, "+sessionModelExpr+" != '') as session_model",
		"min(time_unix_nano) as start_time_unix_nano",
		"max(time_unix_nano) as end_time_unix_nano",
		"toFloat64(max(time_unix_nano) - min(time_unix_nano)) / 1000000000.0 as duration_seconds",
		"toInt64("+sessionMessageCountExpr+") as message_count",
		"toInt64(uniqExactIf("+sessionToolCallDedupIDExpr+", "+sessionCountedToolCallPredicate+")) as tool_call_count",
		sessionInputTokensExpr+" as total_input_tokens",
		sessionOutputTokensExpr+" as total_output_tokens",
		sessionTotalTokensExpr+" as total_tokens",
		sessionCostExpr+" as total_cost",
		"if(countIf("+sessionFailedToolCallPredicate+") > 0, 'error', 'success') as status",
		"toFloat64("+sortExpr+") as sort_value",
	).
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": arg.ProjectIDs}).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where(sessionSourceRowPredicate).
		Where("chat_id != ''")

	sb, err := applySessionFilters(sb, arg.Filters)
	if err != nil {
		return nil, err
	}

	sb = sb.GroupBy("chat_id")

	if arg.CursorSortValue != nil && arg.CursorGramChatID != "" {
		sb = sb.Having("(sort_value, chat_id) < (?, ?)", *arg.CursorSortValue, arg.CursorGramChatID)
	}

	sb = sb.OrderBy("sort_value DESC", "gram_chat_id DESC").
		Limit(uint64(arg.Limit)) //nolint:gosec // Limit is validated by the service layer.

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list sessions query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return scanSessionSummaries(rows)
}

// sessionSummaryMeasureSelects maps the public sort measures onto merged
// aggregates over chat_session_summaries. Keep the key set identical to
// sessionMeasureSelects so both paths accept the same sort_by values. The
// s. qualifier pins identifiers to the source columns: several output aliases
// (total_cost, total_tokens, ...) share the column names, and ClickHouse
// prefers aliases when resolving unqualified identifiers, which would nest
// aggregates (ILLEGAL_AGGREGATION).
//
// #nosec G101 -- These are allowlisted SQL measure expressions, not credentials.
var sessionSummaryMeasureSelects = map[string]string{
	"total_cost":                  "sum(s.total_cost)",
	"total_input_tokens":          "sum(s.total_input_tokens)",
	"total_output_tokens":         "sum(s.total_output_tokens)",
	"total_tokens":                "sum(s.total_tokens)",
	"cache_read_input_tokens":     "sum(s.cache_read_input_tokens)",
	"cache_creation_input_tokens": "sum(s.cache_creation_input_tokens)",
	"tool_call_count":             "uniqExactIfMerge(s.tool_call_count)",
	"message_count":               "uniqExactIfMerge(s.message_count)",
	"duration_seconds":            "toFloat64(max(s.end_time_unix_nano) - min(s.start_time_unix_nano)) / 1000000000.0",
	// Kept as a service-level compatibility alias; the public listSessions API
	// uses tool_call_count.
	"total_tool_calls": "uniqExactIfMerge(s.tool_call_count)",
}

// listSessionsFromSummaries serves the session list from the pre-aggregated
// chat_session_summaries table: one row per (project, hour bucket, chat),
// merged per chat over the buckets inside the window. The window snaps to
// hour-bucket boundaries (start rounds down), so edges carry up to ~1h of
// slop relative to the raw path — acceptable on the wide windows routed here.
//
//nolint:wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) listSessionsFromSummaries(ctx context.Context, arg ListSessionsParams) ([]SessionSummary, error) {
	sortExpr, ok := sessionSummaryMeasureSelects[arg.SortBy]
	if !ok {
		return nil, fmt.Errorf("unknown sort_by measure %q", arg.SortBy)
	}

	sb := sq.Select(
		"s.chat_id as gram_chat_id",
		"any(toString(s.gram_project_id)) as project_id",
		// max() so '' loses to a non-empty value, matching the summary
		// columns' merge semantics.
		"max(s.session_user_email) as session_user_email",
		"max(s.session_hook_source) as session_hook_source",
		"argMaxIfMerge(s.session_model) as session_model",
		"min(s.start_time_unix_nano) as start_time_unix_nano",
		"max(s.end_time_unix_nano) as end_time_unix_nano",
		"toFloat64(max(s.end_time_unix_nano) - min(s.start_time_unix_nano)) / 1000000000.0 as duration_seconds",
		"toInt64(uniqExactIfMerge(s.message_count)) as message_count",
		"toInt64(uniqExactIfMerge(s.tool_call_count)) as tool_call_count",
		"sum(s.total_input_tokens) as total_input_tokens",
		"sum(s.total_output_tokens) as total_output_tokens",
		"sum(s.total_tokens) as total_tokens",
		"sum(s.total_cost) as total_cost",
		"if(sum(s.failed_tool_call_count) > 0, 'error', 'success') as status",
		"toFloat64("+sortExpr+") as sort_value",
	).
		From("chat_session_summaries s").
		Where(squirrel.Eq{"s.gram_project_id": arg.ProjectIDs}).
		Where("s.time_bucket >= toStartOfHour(fromUnixTimestamp64Nano(?, 'UTC'))", arg.TimeStart).
		Where("s.time_bucket <= fromUnixTimestamp64Nano(?, 'UTC')", arg.TimeEnd)

	sb, err := applySessionSummaryFilters(sb, arg.Filters)
	if err != nil {
		return nil, err
	}

	sb = sb.GroupBy("s.chat_id")

	if arg.CursorSortValue != nil && arg.CursorGramChatID != "" {
		sb = sb.Having("(sort_value, s.chat_id) < (?, ?)", *arg.CursorSortValue, arg.CursorGramChatID)
	}

	sb = sb.OrderBy("sort_value DESC", "gram_chat_id DESC").
		Limit(uint64(arg.Limit)) //nolint:gosec // Limit is validated by the service layer.

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list sessions summary query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return scanSessionSummaries(rows)
}

// applySessionSummaryFilters translates the per-chat "any row matches" filter
// semantics of applySessionFilters onto the summary table's merged
// distinct-value arrays. Scalar and array dimensions match via
// hasAny/arrayExists over groupUniqArrayArray of the dimension's column; the
// co-located Claude attribution dimensions match a single per-row tuple in
// attribution_tuples, preserving drill-down semantics from the aggregate
// cost table.
func applySessionSummaryFilters(sb squirrel.SelectBuilder, filters []AttributeMetricsFilter) (squirrel.SelectBuilder, error) {
	var tuplePredicates []string
	var tupleArgs []any

	for _, f := range filters {
		if len(f.Values) == 0 {
			continue
		}
		dim, ok := sessionSummaryDimensionRegistry[f.Dimension]
		if !ok {
			return sb, fmt.Errorf("unknown filter dimension %q", f.Dimension)
		}
		switch {
		case dim.kind == attributeDimProject:
			sb = sb.Where(squirrel.Eq{"s." + dim.column: f.Values})
		case dim.coLocateSessionFilters:
			pred, args := sessionSummaryTupleFieldPredicate(dim.summaryTupleField, f.Values)
			tuplePredicates = append(tuplePredicates, pred)
			tupleArgs = append(tupleArgs, args...)
		default:
			sb = sb.Having(sessionSummaryValuesHaving("s."+dim.column, f.Values))
		}
	}

	if len(tuplePredicates) > 0 {
		sb = sb.Having(squirrel.Expr(
			"arrayExists(t -> "+strings.Join(tuplePredicates, " AND ")+", groupUniqArrayArray(s.attribution_tuples))",
			tupleArgs...,
		))
	}
	return sb, nil
}

// sessionSummaryValuesHaving matches a chat when the merged distinct values
// of a dimension column contain one of the requested values. A requested ""
// (the "(unset)" bucket) matches chats with no non-empty value anywhere,
// mirroring sessionScalarHaving. For array dimensions (roles/groups) this
// means "no value on any row", a deliberate tightening of the raw path's
// per-row empty(...) check, which matches any chat containing a single
// unenriched row.
func sessionSummaryValuesHaving(colExpr string, values []string) squirrel.Sqlizer {
	merged := "groupUniqArrayArray(" + colExpr + ")"

	hasEmpty := false
	nonEmpty := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			hasEmpty = true
			continue
		}
		nonEmpty = append(nonEmpty, v)
	}

	emptyPred := squirrel.Expr("NOT arrayExists(x -> x != '', " + merged + ")")
	if len(nonEmpty) == 0 {
		return emptyPred
	}
	nonEmptyPred := squirrel.Expr("hasAny("+merged+", ?)", nonEmpty)
	if !hasEmpty {
		return nonEmptyPred
	}
	return squirrel.Or{nonEmptyPred, emptyPred}
}

// sessionSummaryTupleFieldPredicate matches one attribution field of a
// per-row tuple inside the arrayExists lambda (t is the lambda argument). A
// requested "" means the row carried no value for the field, mirroring
// sessionScalarRowPredicate. The field name comes from the compile-time
// dimension registry, never from user input.
func sessionSummaryTupleFieldPredicate(field string, values []string) (string, []any) {
	fieldExpr := "tupleElement(t, '" + field + "')"

	hasEmpty := false
	nonEmpty := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			hasEmpty = true
			continue
		}
		nonEmpty = append(nonEmpty, v)
	}

	emptyPred := fieldExpr + " = ''"
	if len(nonEmpty) == 0 {
		return emptyPred, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(nonEmpty)), ",")
	args := make([]any, len(nonEmpty))
	for i, v := range nonEmpty {
		args[i] = v
	}
	nonEmptyPred := fieldExpr + " IN (" + placeholders + ")"
	if !hasEmpty {
		return nonEmptyPred, args
	}
	return "(" + nonEmptyPred + " OR " + emptyPred + ")", args
}

//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func scanSessionSummaries(rows driver.Rows) ([]SessionSummary, error) {
	defer rows.Close()

	var sessions []SessionSummary
	for rows.Next() {
		var session SessionSummary
		if err := rows.ScanStruct(&session); err != nil {
			return nil, fmt.Errorf("scanning session row: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}
