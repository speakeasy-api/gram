package repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
)

const (
	sessionUsageRowPredicate = "(" +
		"startsWith(gram_urn, 'claude-code:usage') OR " +
		"startsWith(gram_urn, 'codex:usage') OR " +
		"startsWith(gram_urn, 'cursor:usage')" +
		")"
	sessionHookToolRowPredicate = "(" +
		"toString(attributes.gram.tool.name) != '' AND " +
		"toString(attributes.gram.tool.name) NOT IN ('claude-code', 'codex', 'cursor')" +
		")"
	sessionCountedToolCallPredicate = "(" +
		sessionHookToolRowPredicate + " AND " +
		"toString(attributes.gram.hook.event) IN ('PostToolUse', 'PostToolUseFailure')" +
		")"
	sessionSourceRowPredicate = "(" + sessionUsageRowPredicate + " OR " + sessionHookToolRowPredicate + ")"
)

// #nosec G101 -- These are allowlisted SQL measure expressions, not credentials.
var sessionMeasureSelects = map[string]string{
	"total_cost":                  "sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '')",
	"total_input_tokens":          "sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '')",
	"total_output_tokens":         "sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '')",
	"total_tokens":                "sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '')",
	"cache_read_input_tokens":     "sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '')",
	"cache_creation_input_tokens": "sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '')",
	"tool_call_count":             "countIf(" + sessionCountedToolCallPredicate + ")",
	"message_count":               "uniqExactIf(toString(attributes.gen_ai.response.id), toString(attributes.gen_ai.response.id) != '')",
	"duration_seconds":            "toFloat64(max(time_unix_nano) - min(time_unix_nano)) / 1000000000.0",
	// Kept as a service-level compatibility alias; the public listSessions API
	// uses tool_call_count.
	"total_tool_calls": "countIf(" + sessionCountedToolCallPredicate + ")",
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

func applySessionFilters(sb squirrel.SelectBuilder, filters []AttributeMetricsFilter) (squirrel.SelectBuilder, error) {
	for _, f := range filters {
		if len(f.Values) == 0 {
			continue
		}
		dim, ok := sessionDimensionRegistry[f.Dimension]
		if !ok {
			return sb, fmt.Errorf("unknown filter dimension %q", f.Dimension)
		}
		switch dim.kind {
		case attributeDimArray:
			sb = sb.Where(arrayDimFilter(dim.column, f.Values))
		case attributeDimScalar, attributeDimProject:
			sb = sb.Where(squirrel.Eq{dim.column: f.Values})
		default:
			return sb, fmt.Errorf("unhandled dimension kind for filter %q", f.Dimension)
		}
	}
	return sb, nil
}

// ListSessions retrieves org-scoped session summaries grouped by chat_id from
// the same source-event classes as attribute_metrics_summaries: usage rows for
// tokens/cost and hook tool rows for tool counts. Pagination is based on the
// selected sort measure plus chat_id so ordering stays stable across pages.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) ListSessions(ctx context.Context, arg ListSessionsParams) ([]SessionSummary, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil
	}

	sortExpr, ok := sessionMeasureSelects[arg.SortBy]
	if !ok {
		return nil, fmt.Errorf("unknown sort_by measure %q", arg.SortBy)
	}

	sb := sq.Select(
		"chat_id as gram_chat_id",
		"any(toString(gram_project_id)) as project_id",
		"anyIf(user_email, user_email != '') as session_user_email",
		"anyIf(hook_source, hook_source != '') as session_hook_source",
		"argMaxIf(toString(attributes.gen_ai.response.model), time_unix_nano, toString(attributes.gen_ai.response.model) != '') as session_model",
		"min(time_unix_nano) as start_time_unix_nano",
		"max(time_unix_nano) as end_time_unix_nano",
		"toFloat64(max(time_unix_nano) - min(time_unix_nano)) / 1000000000.0 as duration_seconds",
		"toInt64(uniqExactIf(toString(attributes.gen_ai.response.id), toString(attributes.gen_ai.response.id) != '')) as message_count",
		"toInt64(countIf("+sessionCountedToolCallPredicate+")) as tool_call_count",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') as total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') as total_output_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') as total_tokens",
		"sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') as total_cost",
		"if(countIf("+sessionCountedToolCallPredicate+" AND (toString(attributes.gram.hook.event) = 'PostToolUseFailure' OR toInt32OrZero(toString(attributes.http.response.status_code)) >= 400)) > 0, 'error', 'success') as status",
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
	defer rows.Close()

	var sessions []SessionSummary
	for rows.Next() {
		var session SessionSummary
		if err = rows.ScanStruct(&session); err != nil {
			return nil, fmt.Errorf("scanning session row: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}
