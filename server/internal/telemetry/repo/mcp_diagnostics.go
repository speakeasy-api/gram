package repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
)

// MCP call outcome classes. The vocabulary is closed and server-side: a
// diagnostic caller receives these names, never a status code or an error
// string, so nothing a provider or a client wrote can reach the result through
// this path.
const (
	// MCPOutcomeSuccess is a 2xx/3xx call, or a hook-observed call that
	// recorded a result.
	MCPOutcomeSuccess = "success"
	// MCPOutcomeUnauthorized is 401/403 — the call was rejected before it ran.
	MCPOutcomeUnauthorized = "unauthorized"
	// MCPOutcomeClientError is any other 4xx: the request itself was rejected.
	MCPOutcomeClientError = "client_error"
	// MCPOutcomeServerError is 5xx. It does not by itself say whether Gram or
	// the upstream provider produced the status, which is why fault
	// attribution weighs it against readiness rather than reading it alone.
	MCPOutcomeServerError = "server_error"
	// MCPOutcomeFailed is a hook-observed call that recorded an error without a
	// status code.
	MCPOutcomeFailed = "failed"
	// MCPOutcomeUnknown is a call whose trace carries neither.
	MCPOutcomeUnknown = "unknown"
)

// MCPClientUnattributed is the client label for calls that arrive straight at
// a hosted MCP server, where nothing today records which client made them.
// Distinct from an empty string so a reader can tell "we do not know" from a
// client that reported no name.
const MCPClientUnattributed = "unattributed"

type GetMCPOutcomeBreakdownParams struct {
	// GramProjectIDs scopes the read. One project answers "this server";
	// an organization's projects answer "is this happening everywhere".
	GramProjectIDs []string
	// ToolsetSlugs matches hosted MCP traffic arriving directly at Gram.
	// Empty selects every server in scope, which is how the organization-wide
	// comparison is taken.
	ToolsetSlugs []string
	// MCPServerURLSuffixes matches the same servers in hook-observed traffic,
	// where the server is identified by the URL the client called (/mcp/<slug>).
	MCPServerURLSuffixes []string
	TimeStart            int64
	TimeEnd              int64
}

type MCPOutcomeBreakdownRow struct {
	Client           string `ch:"client"`
	Outcome          string `ch:"outcome"`
	CallCount        uint64 `ch:"call_count"`
	LastCallUnixNano int64  `ch:"last_call_unix_nano"`
}

// GetMCPOutcomeBreakdown counts calls by outcome class and by the client that
// made them, over two lanes of the same traffic: calls that arrived directly at
// a hosted MCP server (classified by HTTP status, no client attribution
// available yet) and calls observed by an agent hook (classified by
// result/error, attributed to the reporting client).
//
// It returns counts only. No status codes, URLs, arguments, results, or
// identities leave this query.
func (q *Queries) GetMCPOutcomeBreakdown(ctx context.Context, arg GetMCPOutcomeBreakdownParams) ([]MCPOutcomeBreakdownRow, error) {
	if len(arg.GramProjectIDs) == 0 {
		return []MCPOutcomeBreakdownRow{}, nil
	}

	directSQL, directArgs, err := q.mcpOutcomeDirectSource(arg)
	if err != nil {
		return nil, err
	}
	hookSQL, hookArgs, err := q.mcpOutcomeHookSource(arg)
	if err != nil {
		return nil, err
	}

	source := "(" + directSQL + " UNION ALL " + hookSQL + ")"
	sourceArgs := make([]any, 0, len(directArgs)+len(hookArgs))
	sourceArgs = append(sourceArgs, directArgs...)
	sourceArgs = append(sourceArgs, hookArgs...)

	sb := sq.Select(
		"client",
		"outcome",
		"count() AS call_count",
		"max(event_time_ns) AS last_call_unix_nano",
	).
		From(source).
		GroupBy("client", "outcome").
		OrderBy("call_count DESC", "client ASC", "outcome ASC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build mcp outcome breakdown query: %w", err)
	}
	// squirrel places the FROM subquery's placeholders after the outer
	// builder's, and this builder contributes none, so the source arguments
	// lead.
	args = append(sourceArgs, args...)

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query mcp outcome breakdown: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]MCPOutcomeBreakdownRow, 0)
	for rows.Next() {
		var row MCPOutcomeBreakdownRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan mcp outcome breakdown row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mcp outcome breakdown rows: %w", err)
	}
	return result, nil
}

// mcpOutcomeDirectSource classifies calls that reached a hosted MCP server
// directly. Aggregate aliases carry the "g_" prefix for the reason the tool
// usage CTE documents: an alias that shadows a trace_summaries column is merged
// into the enclosing aggregate and fails with ILLEGAL_AGGREGATION.
func (q *Queries) mcpOutcomeDirectSource(arg GetMCPOutcomeBreakdownParams) (string, []any, error) {
	grouped := sq.Select(
		"min(start_time_unix_nano) AS event_time_ns",
		"max(toolset_slug) AS g_toolset_slug",
		"ifNull(anyIfMerge(http_status_code), 0) AS g_http_status_code",
	).
		From("trace_summaries").
		Where(squirrel.Eq{"gram_project_id": arg.GramProjectIDs}).
		GroupBy("trace_id").
		Having("min(start_time_unix_nano) >= ?", arg.TimeStart).
		Having("min(start_time_unix_nano) <= ?", arg.TimeEnd).
		Having("any(event_source) != 'hook'").
		Having("g_toolset_slug != ''")
	if len(arg.ToolsetSlugs) > 0 {
		grouped = grouped.Having(squirrel.Eq{"g_toolset_slug": arg.ToolsetSlugs})
	}
	grouped = withTraceWindowScanBounds(grouped, "start_time_unix_nano", arg.TimeStart, arg.TimeEnd)

	groupedSQL, groupedArgs, err := grouped.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build direct mcp outcome source: %w", err)
	}

	outcome := chMultiIf(
		"g_http_status_code = 401 OR g_http_status_code = 403", "'"+MCPOutcomeUnauthorized+"'",
		"g_http_status_code >= 200 AND g_http_status_code < 400", "'"+MCPOutcomeSuccess+"'",
		"g_http_status_code >= 500", "'"+MCPOutcomeServerError+"'",
		"g_http_status_code >= 400", "'"+MCPOutcomeClientError+"'",
		"'"+MCPOutcomeUnknown+"'",
	)

	return fmt.Sprintf(`
SELECT
	event_time_ns,
	'%s' AS client,
	%s AS outcome
FROM (%s)`, MCPClientUnattributed, outcome, groupedSQL), groupedArgs, nil
}

// mcpOutcomeHookSource classifies the same servers' calls as an agent hook
// observed them. This is the only lane that can name a client today, and it
// names the reporting integration — self-reported evidence, never a metric
// dimension a caller can filter on.
func (q *Queries) mcpOutcomeHookSource(arg GetMCPOutcomeBreakdownParams) (string, []any, error) {
	grouped := sq.Select(
		"min(start_time_unix_nano) AS event_time_ns",
		"any(hook_source) AS g_hook_source",
		"max(mcp_server_url) AS g_mcp_server_url",
		"max(has_result) AS g_has_result",
		"max(has_error) AS g_has_error",
	).
		From("trace_summaries").
		Where(squirrel.Eq{"gram_project_id": arg.GramProjectIDs}).
		GroupBy("trace_id").
		Having("min(start_time_unix_nano) >= ?", arg.TimeStart).
		Having("min(start_time_unix_nano) <= ?", arg.TimeEnd).
		Having("any(event_source) = 'hook'").
		Having("g_mcp_server_url != ''")
	if len(arg.MCPServerURLSuffixes) > 0 {
		grouped = grouped.Having("arrayExists(suffix -> endsWith(g_mcp_server_url, suffix), ?)", arg.MCPServerURLSuffixes)
	}
	grouped = withTraceWindowScanBounds(grouped, "start_time_unix_nano", arg.TimeStart, arg.TimeEnd)

	groupedSQL, groupedArgs, err := grouped.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build hook mcp outcome source: %w", err)
	}

	outcome := chMultiIf(
		"g_has_error = 1", "'"+MCPOutcomeFailed+"'",
		"g_has_result = 1", "'"+MCPOutcomeSuccess+"'",
		"'"+MCPOutcomeUnknown+"'",
	)

	return fmt.Sprintf(`
SELECT
	event_time_ns,
	%s AS client,
	%s AS outcome
FROM (%s)`, chFirstNonEmpty("g_hook_source", "'"+MCPClientUnattributed+"'"), outcome, groupedSQL), groupedArgs, nil
}

type GetTelemetryWatermarkParams struct {
	GramProjectIDs []string
}

// GetTelemetryWatermark returns the newest event time observed for the given
// projects, or zero when they hold no telemetry at all.
//
// Every external diagnostic reports this as its data_through, so a reader can
// tell a quiet system from a stalled pipeline. A zero watermark is the absence
// of observations, never evidence of health.
func (q *Queries) GetTelemetryWatermark(ctx context.Context, arg GetTelemetryWatermarkParams) (int64, error) {
	if len(arg.GramProjectIDs) == 0 {
		return 0, nil
	}

	// gram_project_id leads telemetry_logs' sort key with time_unix_nano next,
	// so this reads the tail of the matching ranges rather than scanning them.
	sb := sq.Select("max(time_unix_nano) AS watermark").
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": arg.GramProjectIDs})

	query, args, err := sb.ToSql()
	if err != nil {
		return 0, fmt.Errorf("build telemetry watermark query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("query telemetry watermark: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var watermark int64
	if rows.Next() {
		if err := rows.Scan(&watermark); err != nil {
			return 0, fmt.Errorf("scan telemetry watermark: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate telemetry watermark: %w", err)
	}
	return watermark, nil
}
