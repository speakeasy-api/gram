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
	// MCPOutcomeServerError is 5xx. It does not by itself say whether Speakeasy or
	// the upstream provider produced the status, which is why fault
	// attribution weighs it against readiness rather than reading it alone.
	MCPOutcomeServerError = "server_error"
	// MCPOutcomeBlocked is a hook-observed call denied by local policy before the
	// upstream server ran.
	MCPOutcomeBlocked = "blocked"
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
	// ToolsetSlugs matches hosted MCP traffic arriving directly at Speakeasy.
	// Empty selects every server in scope, which is how the organization-wide
	// comparison is taken.
	ToolsetSlugs []string
	// MCPServerURLSuffixes matches the same servers in hook-observed traffic,
	// where the server is identified by the URL the client called (/mcp/<slug>).
	MCPServerURLSuffixes []string
	// MCPServerIDs matches calls stamped with gram.mcp_server.id. When set,
	// ToolsetSlugs only matches rows without a server id, so a hosted gateway
	// member's calls, which carry both, stay with the member.
	MCPServerIDs []string
	// ToolSources matches hook-observed calls by the server name the calling
	// agent reported (gram.tool_call.source), by exact value; the caller
	// supplies every spelling a hook reports. A plugin-routed server reaches
	// the hook under a derived prefix such as
	// "plugin_<plugin>_<Display_Name>" and no URL, so this is how those calls
	// are attributed to the configured server.
	ToolSources []string
	// CanonicalIdentityOrg folds linked email aliases when user attribution is
	// selected. Empty preserves literal identities.
	CanonicalIdentityOrg string
	TimeStart            int64
	TimeEnd              int64
	// Limit bounds user and per-user tool attribution rows. Zero preserves the
	// aggregate outcome readers' existing unbounded grouping.
	Limit int
}

// selectsServer reports whether the parameters narrow the read to one server
// through at least one identity. The per-user readers refuse an unscoped read
// rather than broadening to every server in the project.
func (arg GetMCPOutcomeBreakdownParams) selectsServer() bool {
	return arg.selectsDirectLane() || arg.selectsHookLane()
}

// selectsDirectLane reports whether any identity can match a call that arrived
// at Speakeasy directly: a hosted toolset slug or a configured server id.
func (arg GetMCPOutcomeBreakdownParams) selectsDirectLane() bool {
	return len(arg.ToolsetSlugs) > 0 || len(arg.MCPServerIDs) > 0
}

// selectsHookLane reports whether any identity can match a hook-observed call:
// the URL suffix the client called or the server name it reported.
func (arg GetMCPOutcomeBreakdownParams) selectsHookLane() bool {
	return len(arg.MCPServerURLSuffixes) > 0 || len(arg.ToolSources) > 0
}

type MCPOutcomeBreakdownRow struct {
	Client           string `ch:"client"`
	Outcome          string `ch:"outcome"`
	CallCount        uint64 `ch:"call_count"`
	LastCallUnixNano int64  `ch:"last_call_unix_nano"`
}

// MCPUsageUserRow is one observed caller of an MCP server. Raw identities stay
// inside the server process and are converted to short-lived opaque references
// before they reach Platform MCP.
type MCPUsageUserRow struct {
	IdentityKind string `ch:"identity_kind"`
	Identifier   string `ch:"identifier"`
	HasSuccess   bool   `ch:"has_success"`
	HasError     bool   `ch:"has_error"`
	HasBlocked   bool   `ch:"has_blocked"`
	LastUsedAt   int64  `ch:"last_used_at"`
}

// MCPUsageUserToolRow is one caller/tool pair for a selected MCP. It carries
// categories rather than counts so person-level reads cannot reconstruct an
// activity profile.
type MCPUsageUserToolRow struct {
	IdentityKind string `ch:"identity_kind"`
	Identifier   string `ch:"identifier"`
	ToolName     string `ch:"tool_name"`
	HasSuccess   bool   `ch:"has_success"`
	HasError     bool   `ch:"has_error"`
	HasBlocked   bool   `ch:"has_blocked"`
}

// GetMCPOutcomeBreakdown counts calls by outcome class and by the client that
// made them, over two lanes of the same traffic: calls that arrived directly at
// a hosted or proxied MCP server (classified by HTTP status, no client
// attribution available yet) and calls observed by an agent hook (classified by
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

// ListMCPUsageUsers returns callers observed against one selected MCP server.
// The source is already server-scoped at row level, so a shared session cannot
// pull another server's users or outcomes into the result.
func (q *Queries) ListMCPUsageUsers(ctx context.Context, arg GetMCPOutcomeBreakdownParams) ([]MCPUsageUserRow, error) {
	if len(arg.GramProjectIDs) == 0 || !arg.selectsServer() {
		return []MCPUsageUserRow{}, nil
	}

	source, sourceArgs, err := q.mcpTraceSource(arg)
	if err != nil {
		return nil, err
	}
	orgLit := canonicalIdentityOrgLiteral(arg.CanonicalIdentityOrg)
	userEmail := "user_email"
	if orgLit != "" {
		userEmail = canonicalEmailExpr(orgLit, "user_email")
	}
	identityKind := chMultiIf(
		userEmail+" != ''", "'email'",
		"external_user_id != ''", "'external'",
		"user_id != ''", "'user'",
		"''",
	)
	identifier := chFirstNonEmpty(userEmail, "external_user_id", "user_id", "''")
	sb := sq.Select(
		identityKind+" AS identity_kind",
		identifier+" AS identifier",
		"countIf(outcome = '"+MCPOutcomeSuccess+"') > 0 AS has_success",
		"countIf(outcome IN ('"+MCPOutcomeUnauthorized+"', '"+MCPOutcomeClientError+"', '"+MCPOutcomeServerError+"', '"+MCPOutcomeFailed+"')) > 0 AS has_error",
		"countIf(outcome = '"+MCPOutcomeBlocked+"') > 0 AS has_blocked",
		"max(event_time_ns) AS last_used_at",
	).
		From(source).
		Where("identifier != ''").
		GroupBy("identity_kind", "identifier").
		OrderBy("last_used_at DESC", "identity_kind ASC", "identifier ASC")
	if arg.Limit > 0 {
		sb = sb.Limit(uint64(arg.Limit))
	}
	sb = withCanonicalFoldSettings(sb, orgLit)
	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build mcp usage users query: %w", err)
	}
	args = append(sourceArgs, args...)
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query mcp usage users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]MCPUsageUserRow, 0)
	for rows.Next() {
		var row MCPUsageUserRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan mcp usage user row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mcp usage user rows: %w", err)
	}
	return result, nil
}

// ListMCPUsageUserTools returns categorical tool outcomes for one already
// resolved identity. Empty identity input returns no rows rather than dropping
// the filter and broadening to every caller.
func (q *Queries) ListMCPUsageUserTools(ctx context.Context, arg GetMCPOutcomeBreakdownParams, identityKind, identifier string) ([]MCPUsageUserToolRow, error) {
	if len(arg.GramProjectIDs) == 0 || !arg.selectsServer() || identifier == "" {
		return []MCPUsageUserToolRow{}, nil
	}

	source, sourceArgs, err := q.mcpTraceSource(arg)
	if err != nil {
		return nil, err
	}
	var column string
	orgLit := canonicalIdentityOrgLiteral(arg.CanonicalIdentityOrg)
	switch identityKind {
	case "email":
		column = "user_email"
	case "external":
		column = "external_user_id"
	case "user":
		column = "user_id"
	default:
		return []MCPUsageUserToolRow{}, nil
	}
	sb := sq.Select(
		"'"+identityKind+"' AS identity_kind",
		column+" AS identifier",
		"tool_name",
		"countIf(outcome = '"+MCPOutcomeSuccess+"') > 0 AS has_success",
		"countIf(outcome IN ('"+MCPOutcomeUnauthorized+"', '"+MCPOutcomeClientError+"', '"+MCPOutcomeServerError+"', '"+MCPOutcomeFailed+"')) > 0 AS has_error",
		"countIf(outcome = '"+MCPOutcomeBlocked+"') > 0 AS has_blocked",
	).
		From(source).
		Where("tool_name != ''")
	if identityKind == "email" && orgLit != "" {
		sb = sb.Where(canonicalEmailPredicate(orgLit, column, []string{identifier}))
	} else {
		sb = sb.Where(column+" = ?", identifier)
	}
	sb = sb.
		GroupBy(column, "tool_name").
		OrderBy("has_error DESC", "has_blocked DESC", "tool_name ASC")
	if arg.Limit > 0 {
		sb = sb.Limit(uint64(arg.Limit))
	}
	sb = withCanonicalFoldSettings(sb, orgLit)
	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build mcp usage user tools query: %w", err)
	}
	args = append(sourceArgs, args...)
	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query mcp usage user tools: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]MCPUsageUserToolRow, 0)
	for rows.Next() {
		var row MCPUsageUserToolRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan mcp usage user tool row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mcp usage user tool rows: %w", err)
	}
	return result, nil
}

// mcpTraceSource builds the per-call row set both diagnostics and drill-down
// read from: one row per call, carrying its correlation id, when it happened,
// the client and user that made it, the tool it called, and its outcome.
// Aggregations sit on top of this bounded, server-filtered row set.
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

// mcpOutcomeDirectSource classifies calls that reached a hosted or proxied MCP
// server directly. Diagnostics need call-level attribution: trace_summaries has already
// collapsed a trace's server, tool, and user dimensions with any(), so a trace
// that contains more than one call cannot be filtered truthfully there. The raw
// read is bounded to the diagnostic window and narrows by server before it
// groups lifecycle rows into one call.
func (q *Queries) mcpOutcomeDirectSource(arg GetMCPOutcomeBreakdownParams) (string, []any, error) {
	httpStatusCode := "toInt32OrZero(toString(attributes.http.response.status_code))"
	eventID := chFirstNonEmpty("toString(span_id)", "toString(trace_id)")
	grouped := sq.Select(
		"trace_id",
		eventID+" AS event_id",
		"min(time_unix_nano) AS event_time_ns",
		"max(tool_name) AS g_tool_name",
		"max(user_email) AS g_user_email",
		"max(external_user_id) AS g_external_user_id",
		"max(user_id) AS g_user_id",
		"max("+httpStatusCode+") AS g_http_status_code",
	).
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": arg.GramProjectIDs}).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where("trace_id IS NOT NULL").
		Where("trace_id != ''").
		Where("event_source != 'hook'")
	switch {
	case arg.selectsDirectLane():
		// A hosted server is recorded under its toolset slug; a proxied remote,
		// tunneled, or gateway-member server under its configured id. A row
		// carries whichever applies, so matching either never counts one twice.
		predicate := squirrel.Or{}
		if len(arg.ToolsetSlugs) > 0 {
			predicate = append(predicate, toolsetSlugArm(arg.ToolsetSlugs, len(arg.MCPServerIDs) > 0))
		}
		if len(arg.MCPServerIDs) > 0 {
			predicate = append(predicate, squirrel.Eq{"mcp_server_id": arg.MCPServerIDs})
		}
		grouped = grouped.Where(predicate)
	case arg.selectsHookLane():
		// This selected MCP has only hook-observed identities. The direct lane
		// cannot prove a match, so exclude it rather than broadening to every
		// call in the project.
		grouped = grouped.Where("0")
	default:
		// Unfiltered is the organization-wide comparison: every call that names
		// a server, whichever identity it carries.
		grouped = grouped.Where("(toolset_slug != '' OR mcp_server_id != '')")
	}
	grouped = grouped.GroupBy("trace_id", eventID)

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
	trace_id,
	event_id,
	event_time_ns,
	'%s' AS client,
	g_tool_name AS tool_name,
	g_user_email AS user_email,
	g_external_user_id AS external_user_id,
	g_user_id AS user_id,
	%s AS outcome
FROM (%s)`, MCPClientUnattributed, outcome, groupedSQL), groupedArgs, nil
}

// mcpOutcomeHookSource classifies the same servers' calls as an agent hook
// observed them. A real tool-call id joins request/result/error rows. Older
// senders without one fall back to the trace id, preserving the established
// conservative behavior of counting repeated same-tool calls as one rather
// than manufacturing calls from lifecycle rows.
func (q *Queries) mcpOutcomeHookSource(arg GetMCPOutcomeBreakdownParams) (string, []any, error) {
	hasResult := "toUInt8(toString(attributes.gen_ai.tool.call.result) != '')"
	hasError := "toUInt8(toString(attributes.gram.hook.error) != '')"
	hasBlock := "toUInt8(toString(attributes.gram.hook.block_reason) != '')"
	toolCallID := chFirstNonEmpty(
		"toString(attributes.gen_ai.tool.call.id)",
		"toString(attributes.tool_use_id)",
		"toString(trace_id)",
	)
	grouped := sq.Select(
		"trace_id",
		toolCallID+" AS event_id",
		"min(time_unix_nano) AS event_time_ns",
		"max(hook_source) AS g_hook_source",
		"max(tool_name) AS g_tool_name",
		"max(user_email) AS g_user_email",
		"max(external_user_id) AS g_external_user_id",
		"max(user_id) AS g_user_id",
		"max("+hasResult+") AS g_has_result",
		"max("+hasError+") AS g_has_error",
		"max("+hasBlock+") AS g_has_block",
	).
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": arg.GramProjectIDs}).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where("trace_id IS NOT NULL").
		Where("trace_id != ''").
		Where("event_source = 'hook'")
	switch {
	case arg.selectsHookLane():
		// A hook that resolved the server records the URL it called; one that
		// did not (a plugin-shipped server, for instance) records only the name
		// the agent used. Either identifies this server.
		predicate := squirrel.Or{}
		if len(arg.MCPServerURLSuffixes) > 0 {
			predicate = append(predicate, hookServerURLMatch(arg.MCPServerURLSuffixes))
		}
		if len(arg.ToolSources) > 0 {
			predicate = append(predicate, hookToolSourceMatch(arg.ToolSources))
		}
		grouped = grouped.Where(predicate)
	case arg.selectsDirectLane():
		// This selected MCP has only direct identities. The hook lane cannot
		// prove a match, so exclude it rather than broadening to every
		// hook-observed server in the project.
		grouped = grouped.Where("0")
	default:
		// Unfiltered is the organization-wide comparison: every hook-observed
		// call whose server the hook resolved.
		grouped = grouped.Where(hookServerURLExpr + " != ''")
	}
	grouped = grouped.GroupBy("trace_id", toolCallID)

	groupedSQL, groupedArgs, err := grouped.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build hook mcp outcome source: %w", err)
	}

	outcome := chMultiIf(
		"g_has_block = 1", "'"+MCPOutcomeBlocked+"'",
		"g_has_error = 1", "'"+MCPOutcomeFailed+"'",
		"g_has_result = 1", "'"+MCPOutcomeSuccess+"'",
		"'"+MCPOutcomeUnknown+"'",
	)

	return fmt.Sprintf(`
SELECT
	trace_id,
	event_id,
	event_time_ns,
	%s AS client,
	g_tool_name AS tool_name,
	g_user_email AS user_email,
	g_external_user_id AS external_user_id,
	g_user_id AS user_id,
	%s AS outcome
FROM (%s)`, chFirstNonEmpty("g_hook_source", "'"+MCPClientUnattributed+"'"), outcome, groupedSQL), groupedArgs, nil
}

// toolsetSlugArm matches hosted rows by toolset slug. Paired with a server id
// arm, it leaves out rows stamped with another server's id, such as a gateway
// member fronting the same toolset.
func toolsetSlugArm(slugs []string, withServerArm bool) squirrel.Sqlizer {
	if !withServerArm {
		return squirrel.Eq{"toolset_slug": slugs}
	}
	return squirrel.And{squirrel.Eq{"toolset_slug": slugs}, squirrel.Eq{"mcp_server_id": ""}}
}

// hookServerURLExpr is the URL a hook-observed client called, as the hook
// recorded it; empty when the hook never resolved the server to a URL.
var hookServerURLExpr = chAttr("gram.mcp.server_url")

// hookServerURLMatch matches hook-observed rows whose recorded server URL
// (gram.mcp.server_url) ends in one of suffixes, the /mcp/<slug> form a hook
// that resolved the server records.
func hookServerURLMatch(suffixes []string) squirrel.Sqlizer {
	return squirrel.Expr(
		"("+hookServerURLExpr+" != '' AND arrayExists(suffix -> endsWith("+hookServerURLExpr+", suffix), ?))",
		suffixes,
	)
}

// hookToolSourceMatch matches hook-observed rows by the server name the agent
// reported (gram.tool_call.source), by exact value: the resolver supplies every
// spelling a hook reports, so the bare materialized column, and its bloom
// filter index, does the comparison.
func hookToolSourceMatch(sources []string) squirrel.Sqlizer {
	return squirrel.Eq{"tool_source": sources}
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
