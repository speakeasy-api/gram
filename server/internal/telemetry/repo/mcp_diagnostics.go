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

	source, sourceArgs := unionSource(directSQL, directArgs, hookSQL, hookArgs)

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

// mcpTraceSource builds the per-trace row set both diagnostics and drill-down
// read from: one row per trace, carrying its correlation id, when it happened,
// the client that made it, the tool it called, and its classified outcome.
// Aggregations sit on top of this; nothing below it reaches a raw log row.
func (q *Queries) mcpTraceSource(arg GetMCPOutcomeBreakdownParams) (string, []any, error) {
	directSQL, directArgs, err := q.mcpOutcomeDirectSource(arg)
	if err != nil {
		return "", nil, err
	}
	hookSQL, hookArgs, err := q.mcpOutcomeHookSource(arg)
	if err != nil {
		return "", nil, err
	}
	source, sourceArgs := unionSource(directSQL, directArgs, hookSQL, hookArgs)
	return source, sourceArgs, nil
}

func unionSource(directSQL string, directArgs []any, hookSQL string, hookArgs []any) (string, []any) {
	args := make([]any, 0, len(directArgs)+len(hookArgs))
	args = append(args, directArgs...)
	args = append(args, hookArgs...)
	return "(" + directSQL + " UNION ALL " + hookSQL + ")", args
}

// mcpOutcomeDirectSource classifies calls that reached a hosted MCP server
// directly. It queries telemetry_logs directly and filters by toolset_slug at
// the row level to ensure correct per-tool attribution when a trace contains
// calls to multiple MCP servers.
//
// The query groups by trace_id to deduplicate multiple log rows for the same
// call, but the toolset_slug filter is applied before grouping so only rows
// matching the specified server are included.
func (q *Queries) mcpOutcomeDirectSource(arg GetMCPOutcomeBreakdownParams) (string, []any, error) {
	httpStatusCode := "toInt32OrZero(toString(attributes.http.response.status_code))"

	sb := sq.Select(
		"trace_id",
		"min(time_unix_nano) AS event_time_ns",
		"max(toolset_slug) AS g_toolset_slug",
		"max(tool_name) AS g_tool_name",
		"max("+httpStatusCode+") AS g_http_status_code",
	).
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": arg.GramProjectIDs}).
		Where("trace_id IS NOT NULL").
		Where("trace_id != ''").
		Where("toolset_slug != ''").
		Where("event_source != 'hook'")

	if len(arg.ToolsetSlugs) > 0 {
		sb = sb.Where(squirrel.Eq{"toolset_slug": arg.ToolsetSlugs})
	}

	sb = sb.GroupBy("trace_id").
		Having("min(time_unix_nano) >= ?", arg.TimeStart).
		Having("min(time_unix_nano) <= ?", arg.TimeEnd)

	sb = withTraceWindowScanBounds(sb, "time_unix_nano", arg.TimeStart, arg.TimeEnd)

	groupedSQL, groupedArgs, err := sb.ToSql()
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
	trace_id,
	event_time_ns,
	'%s' AS client,
	g_tool_name AS tool_name,
	%s AS outcome
FROM (%s)`, MCPClientUnattributed, outcome, groupedSQL), groupedArgs, nil
}

// mcpOutcomeHookSource classifies the same servers' calls as an agent hook
// observed them. This is the only lane that can name a client today, and it
// names the reporting integration — self-reported evidence, never a metric
// dimension a caller can filter on.
//
// It queries telemetry_logs directly and filters by mcp_server_url at the row
// level to ensure correct per-tool attribution when a trace (session) contains
// calls to multiple MCP servers. Grouping uses a tool call dedup identifier
// (gen_ai.tool.call.id or tool_use_id) to distinguish individual tool calls
// within a trace.
func (q *Queries) mcpOutcomeHookSource(arg GetMCPOutcomeBreakdownParams) (string, []any, error) {
	mcpServerURL := "toString(attributes.gram.mcp.server_url)"
	hasResult := "if(toString(attributes.gen_ai.tool.call.result) != '', 1, 0)"
	hasError := "if(toString(attributes.gram.hook.error) != '', 1, 0)"
	// Tool call dedup identifier: prefer tool_use_id, then gen_ai.tool.call.id,
	// fallback to row id. This mirrors the session schema's tool_call_dedup_id.
	toolCallDedupID := "multiIf(" +
		"toString(attributes.tool_use_id) != '', toString(attributes.tool_use_id), " +
		"toString(attributes.gen_ai.tool.call.id) != '', toString(attributes.gen_ai.tool.call.id), " +
		"toString(id))"

	sb := sq.Select(
		"trace_id",
		"min(time_unix_nano) AS event_time_ns",
		"max(hook_source) AS g_hook_source",
		"max(tool_name) AS g_tool_name",
		"max("+mcpServerURL+") AS g_mcp_server_url",
		"max("+hasResult+") AS g_has_result",
		"max("+hasError+") AS g_has_error",
	).
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": arg.GramProjectIDs}).
		Where("trace_id IS NOT NULL").
		Where("trace_id != ''").
		Where("event_source = 'hook'").
		Where(mcpServerURL + " != ''")

	if len(arg.MCPServerURLSuffixes) > 0 {
		sb = sb.Where("arrayExists(suffix -> endsWith("+mcpServerURL+", suffix), ?)", arg.MCPServerURLSuffixes)
	}

	// Group by both trace_id and tool_call_dedup_id to distinguish individual
	// tool calls within a session that may call the same server multiple times.
	sb = sb.GroupBy("trace_id", toolCallDedupID).
		Having("min(time_unix_nano) >= ?", arg.TimeStart).
		Having("min(time_unix_nano) <= ?", arg.TimeEnd)

	sb = withTraceWindowScanBounds(sb, "time_unix_nano", arg.TimeStart, arg.TimeEnd)

	groupedSQL, groupedArgs, err := sb.ToSql()
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
	trace_id,
	event_time_ns,
	%s AS client,
	g_tool_name AS tool_name,
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
