package repo

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Masterminds/squirrel"
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
//
//nolint:wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) InsertTelemetryLog(ctx context.Context, arg InsertTelemetryLogParams) error {
	ctx = clickhouse.Context(ctx, clickhouse.WithAsync(false))

	query, args, err := sq.Insert("telemetry_logs").
		Columns(
			"id",
			"time_unix_nano",
			"observed_time_unix_nano",
			"severity_text",
			"body",
			"trace_id",
			"span_id",
			"attributes",
			"resource_attributes",
			"gram_project_id",
			"gram_deployment_id",
			"gram_function_id",
			"gram_urn",
			"service_name",
			"service_version",
			"gram_chat_id",
		).
		Values(
			arg.ID,
			arg.TimeUnixNano,
			arg.ObservedTimeUnixNano,
			arg.SeverityText,
			arg.Body,
			arg.TraceID,
			arg.SpanID,
			arg.Attributes,
			arg.ResourceAttributes,
			arg.GramProjectID,
			arg.GramDeploymentID,
			arg.GramFunctionID,
			arg.GramURN,
			arg.ServiceName,
			arg.ServiceVersion,
			arg.GramChatID,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("building insert query: %w", err)
	}

	return q.conn.Exec(ctx, query, args...)
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
	GramProjectID   string
	TimeStart       int64
	TimeEnd         int64
	IntervalSeconds int64  // Bucket interval in seconds
	ExternalUserID  string // Optional filter
	APIKeyID        string // Optional filter
	ToolsetSlug     string // Optional filter - filters by toolset/MCP server slug
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
			"avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(gram_urn, 'tools:')) AS avg_tool_latency_ms",
			"avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gen_ai.conversation.duration) != '') AS avg_session_duration_ms",
		).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		GroupBy("bucket_time_unix_nano")

	if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}
	if arg.APIKeyID != "" {
		sb = sb.Where(squirrel.Eq{"api_key_id": arg.APIKeyID})
	}
	if arg.ToolsetSlug != "" {
		sb = sb.Where(squirrel.Eq{"toolset_slug": arg.ToolsetSlug})
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
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	ExternalUserID string // Optional filter
	APIKeyID       string // Optional filter
	ToolsetSlug    string // Optional filter - filters by toolset/MCP server slug
	Limit          int
	SortBy         string // "count" or "failure_rate"
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
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
	}
	if arg.APIKeyID != "" {
		sb = sb.Where(squirrel.Eq{"api_key_id": arg.APIKeyID})
	}
	if arg.ToolsetSlug != "" {
		sb = sb.Where(squirrel.Eq{"toolset_slug": arg.ToolsetSlug})
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
	GramProjectID  string
	TimeStart      int64
	TimeEnd        int64
	ExternalUserID string // Optional filter
	APIKeyID       string // Optional filter
	ToolsetSlug    string // Optional filter - filters by toolset/MCP server slug
}

// GetOverviewSummary retrieves aggregated summary metrics for the observability overview.
// When no filters are applied, reads from the pre-aggregated metrics_summaries MV.
// Falls back to scanning telemetry_logs when external_user_id or api_key_id filters are set.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetOverviewSummary(ctx context.Context, arg GetOverviewSummaryParams) (*OverviewSummary, error) {
	hasFilters := arg.ExternalUserID != "" || arg.APIKeyID != "" || arg.ToolsetSlug != ""

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
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gen_ai.conversation.duration) != '') as avg_session_duration_ms",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, evaluation_score_label = 'success') as avg_resolution_time_ms",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') as total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') as total_output_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') as total_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '') as cache_read_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '') as cache_creation_input_tokens",
		"sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') as total_cost",
		"countIf(startsWith(gram_urn, 'tools:')) as total_tool_calls",
		"countIf(startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) as failed_tool_calls",
		"avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(gram_urn, 'tools:')) as avg_latency_ms",
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
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') as total_tokens",
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
	sb = withHavingTuplePagination(sb, arg.Cursor, arg.SortOrder, arg.GramProjectID, "gram_chat_id", "min(time_unix_nano)")

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

	println("\n\n\n", strings.Join(arg.ChatIDs, ", "), "\n\n\n")

	sb := sq.Select(
		"gram_chat_id",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') as total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') as total_output_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') as total_tokens",
		"sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') as total_cost",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where(squirrel.Eq{"gram_chat_id": arg.ChatIDs}).
		GroupBy("gram_chat_id")

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

// SearchUsersParams contains the parameters for searching users with aggregated metrics.
type SearchUsersParams struct {
	GramProjectID    string
	TimeStart        int64
	TimeEnd          int64
	GramDeploymentID string // optional
	GroupBy          string // "user_id" or "external_user_id"
	SortOrder        string // "asc" or "desc"
	Cursor           string // user identifier to paginate from
	Limit            int
}

// SearchUsers retrieves aggregated usage metrics grouped by user identifier.
//
// Groups telemetry logs by user_id or external_user_id and computes per-user
// metrics including tokens, chats, and tool call breakdowns.
// Pagination uses last_seen_unix_nano + the group column for stable cursor ordering.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) SearchUsers(ctx context.Context, arg SearchUsersParams) ([]UserSummary, error) {
	groupCol := "user_id"
	if arg.GroupBy == "external_user_id" {
		groupCol = "external_user_id"
	}

	sb := sq.Select(
		groupCol+" AS user_id",

		// Activity timestamps
		"min(time_unix_nano) AS first_seen_unix_nano",
		"max(time_unix_nano) AS last_seen_unix_nano",

		// Chat metrics
		"uniqExactIf(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats",
		"uniqExactIf(toString(attributes.gen_ai.response.id), toString(attributes.gen_ai.response.id) != '') AS total_chat_requests",

		// Token metrics (from any event with gen_ai usage data)
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gen_ai.usage.input_tokens) != '') AS total_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gen_ai.usage.output_tokens) != '') AS total_output_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_read.input_tokens)), toString(attributes.gen_ai.usage.cache_read.input_tokens) != '') AS cache_read_input_tokens",
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.cache_creation.input_tokens)), toString(attributes.gen_ai.usage.cache_creation.input_tokens) != '') AS cache_creation_input_tokens",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS avg_tokens_per_request",
		"sumIf(toFloat64OrZero(toString(attributes.gen_ai.usage.cost)), toString(attributes.gen_ai.usage.cost) != '') AS total_cost",

		// Tool call metrics
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS total_tool_calls",
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_call_success",
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_call_failure",

		// Tool breakdowns (maps of tool URN -> count)
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:')) AS tool_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_success_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_failure_counts",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where(groupCol + " IS NOT NULL").
		Where(groupCol + " != ''")

	// Optional deployment filter
	if arg.GramDeploymentID != "" {
		sb = sb.Where("gram_deployment_id = toUUIDOrNull(?)", arg.GramDeploymentID)
	}

	sb = sb.GroupBy(groupCol)

	// Cursor pagination using last_seen + group column for stable ordering
	sb = withHavingTuplePagination(sb, arg.Cursor, arg.SortOrder, arg.GramProjectID, groupCol, "max(time_unix_nano)")

	// Order by last_seen with group column as tie-breaker
	sb = withOrdering(sb, arg.SortOrder, "last_seen_unix_nano", groupCol)

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
}

// GetUserMetricsSummary retrieves aggregated metrics for a specific user.
// Uses the same aggregations as GetMetricsSummary (project metrics) but filtered by user.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetUserMetricsSummary(ctx context.Context, arg GetUserMetricsSummaryParams) (*MetricsSummaryRow, error) {
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
		"sumIf(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS total_tokens",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gen_ai.usage.total_tokens) != '') AS avg_tokens_per_request",

		// Chat request metrics
		"uniqExactIf(toString(attributes.gen_ai.response.id), toString(attributes.gen_ai.response.id) != '') AS total_chat_requests",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gen_ai.conversation.duration) != '') AS avg_chat_duration_ms",

		// Resolution status
		"countIf(position(toString(attributes.gen_ai.response.finish_reasons), 'stop') > 0) AS finish_reason_stop",
		"countIf(position(toString(attributes.gen_ai.response.finish_reasons), 'tool_calls') > 0) AS finish_reason_tool_calls",

		// Tool call metrics
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS total_tool_calls",
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_call_success",
		"countIf(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_call_failure",
		"avgIf(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS avg_tool_duration_ms",

		// Chat resolution metrics (from AI evaluation of chat outcomes)
		"countIf(evaluation_score_label = 'success') AS chat_resolution_success",
		"countIf(evaluation_score_label = 'failure') AS chat_resolution_failure",
		"countIf(evaluation_score_label = 'partial') AS chat_resolution_partial",
		"countIf(evaluation_score_label = 'abandoned') AS chat_resolution_abandoned",
		"avgIf(toFloat64OrZero(toString(attributes.gen_ai.evaluation.score.value)), evaluation_score_label != '') AS avg_chat_resolution_score",

		// Model breakdown (map of model name -> count)
		"sumMapIf(map(toString(attributes.gen_ai.response.model), toUInt64(1)), toString(attributes.gen_ai.response.model) != '') AS models",

		// Tool breakdowns (maps of tool URN -> count)
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:')) AS tool_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_success_counts",
		"sumMapIf(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_failure_counts",
	).
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd)

	// Filter by user ID (one of these must be set)
	if arg.UserID != "" {
		sb = sb.Where(squirrel.Eq{"user_id": arg.UserID})
	} else if arg.ExternalUserID != "" {
		sb = sb.Where(squirrel.Eq{"external_user_id": arg.ExternalUserID})
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

// ListFilterOptionsParams contains the parameters for listing filter options.
type ListFilterOptionsParams struct {
	GramProjectID string
	TimeStart     int64
	TimeEnd       int64
	FilterType    string // "api_key" or "user"
	Limit         int
}

// ListFilterOptions retrieves distinct filter values (API keys or users) for a time period.
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
		"multiIf(max(has_blocked) = 1, 'blocked', max(has_error) = 1, 'failure', max(has_result) = 1, 'success', 'pending') as hook_status",
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
		activityColumn = "countIf(toString(attributes.gram.resource.urn) = 'agents:chat:completion') as activity_count"
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
		activityColumn = "countIf(toString(attributes.gram.resource.urn) = 'agents:chat:completion') as activity_count"
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
		userCountCondition = "uniqExactIf(if(external_user_id != '', external_user_id, user_id), toString(attributes.gram.resource.urn) = 'agents:chat:completion' AND if(external_user_id != '', external_user_id, user_id) != '')"
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
