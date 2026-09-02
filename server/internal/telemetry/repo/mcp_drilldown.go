package repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
)

// maxMCPTraceReferences caps a single page of trace references. Drill-down
// exists to point at a handful of concrete occurrences, not to export a log.
const maxMCPTraceReferences = 50

type GetMCPToolOutcomeBreakdownParams struct {
	GetMCPOutcomeBreakdownParams
}

type MCPToolOutcomeBreakdownRow struct {
	ToolName  string `ch:"tool_name"`
	Outcome   string `ch:"outcome"`
	CallCount uint64 `ch:"call_count"`
}

// GetMCPToolOutcomeBreakdown counts one MCP's calls per tool and outcome class,
// so a caller that already knows a server is failing can find which of its
// tools accounts for it.
//
// Tool names are Gram-side configuration, not caller-supplied content, which is
// why they may be projected where arguments and results may not.
func (q *Queries) GetMCPToolOutcomeBreakdown(ctx context.Context, arg GetMCPToolOutcomeBreakdownParams) ([]MCPToolOutcomeBreakdownRow, error) {
	if len(arg.GramProjectIDs) == 0 {
		return []MCPToolOutcomeBreakdownRow{}, nil
	}

	source, sourceArgs, err := q.mcpTraceSource(arg.GetMCPOutcomeBreakdownParams)
	if err != nil {
		return nil, err
	}

	sb := sq.Select(
		"tool_name",
		"outcome",
		"count() AS call_count",
	).
		From(source).
		Where("tool_name != ''").
		GroupBy("tool_name", "outcome").
		OrderBy("call_count DESC", "tool_name ASC", "outcome ASC")

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build mcp tool outcome breakdown query: %w", err)
	}
	args = append(sourceArgs, args...)

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query mcp tool outcome breakdown: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]MCPToolOutcomeBreakdownRow, 0)
	for rows.Next() {
		var row MCPToolOutcomeBreakdownRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan mcp tool outcome breakdown row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mcp tool outcome breakdown rows: %w", err)
	}
	return result, nil
}

type ListMCPTraceReferencesParams struct {
	GetMCPOutcomeBreakdownParams
	// Outcomes narrows to specific classes. Empty returns every class, which is
	// rarely what a drill-down wants — the caller usually arrives here holding a
	// failure class the overview named.
	Outcomes []string
	// BeforeUnixNano, BeforeTraceID, and BeforeToolCallID continue a previous
	// page. They are the composite key the ordering below uses. Paging on the
	// timestamp alone would skip every remaining call sharing the boundary
	// nanosecond. Paging on (time, trace_id) alone would skip remaining calls
	// in the same session — hook-observed traffic shares a trace_id across
	// every tool call in the session.
	BeforeUnixNano   int64
	BeforeTraceID    string
	BeforeToolCallID string
	Limit            int
}

type MCPTraceReferenceRow struct {
	TraceID    string `ch:"trace_id"`
	ToolCallID string `ch:"tool_call_id"`
	OccurredAt int64  `ch:"event_time_ns"`
	ToolName   string `ch:"tool_name"`
	Outcome    string `ch:"outcome"`
	ClientName string `ch:"client"`
}

// ListMCPTraceReferences returns individual traces for one MCP, newest first,
// reduced to a correlation id and a classification.
//
// It is deliberately not a log reader: no body, no attributes, no arguments, no
// result, no URL, no identity. The trace id is a correlation reference — the
// thing a caller quotes when escalating — and everything else is a closed-set
// classification the server assigned.
func (q *Queries) ListMCPTraceReferences(ctx context.Context, arg ListMCPTraceReferencesParams) ([]MCPTraceReferenceRow, error) {
	if len(arg.GramProjectIDs) == 0 {
		return []MCPTraceReferenceRow{}, nil
	}

	source, sourceArgs, err := q.mcpTraceSource(arg.GetMCPOutcomeBreakdownParams)
	if err != nil {
		return nil, err
	}

	limit := arg.Limit
	if limit <= 0 || limit > maxMCPTraceReferences {
		limit = maxMCPTraceReferences
	}

	sb := sq.Select(
		"trace_id",
		"tool_call_id",
		"event_time_ns",
		"tool_name",
		"outcome",
		"client",
	).
		From(source)
	if len(arg.Outcomes) > 0 {
		sb = sb.Where(squirrel.Eq{"outcome": arg.Outcomes})
	}
	if arg.BeforeUnixNano > 0 {
		if arg.BeforeTraceID != "" && arg.BeforeToolCallID != "" {
			sb = sb.Where("(event_time_ns, trace_id, tool_call_id) < (?, ?, ?)", arg.BeforeUnixNano, arg.BeforeTraceID, arg.BeforeToolCallID)
		} else if arg.BeforeTraceID != "" {
			// A cursor without a call id keeps the (time, trace) exclusive
			// boundary so a token minted before that half existed still pages
			// one-call-per-trace traffic correctly.
			sb = sb.Where("(event_time_ns, trace_id) < (?, ?)", arg.BeforeUnixNano, arg.BeforeTraceID)
		} else {
			sb = sb.Where("event_time_ns < ?", arg.BeforeUnixNano)
		}
	}
	// Ordered by time, then trace, then call so a page boundary is
	// deterministic when several calls share a session and a nanosecond.
	sb = sb.OrderBy("event_time_ns DESC", "trace_id DESC", "tool_call_id DESC").Limit(uint64(limit))

	query, args, err := sb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build mcp trace reference query: %w", err)
	}
	args = append(sourceArgs, args...)

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query mcp trace references: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]MCPTraceReferenceRow, 0, limit)
	for rows.Next() {
		var row MCPTraceReferenceRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan mcp trace reference row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mcp trace reference rows: %w", err)
	}
	return result, nil
}
