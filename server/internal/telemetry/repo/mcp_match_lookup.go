package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MCPProvenance is the server-level identity the hook recorded for one MCP
// tool call.
//
// Match is the `gram.mcp.match` attribute: an HTTP/SSE URL, a stdio launch
// command, or — when the sender's inventory snapshot didn't resolve the
// server — the bare `mcp__<server>__` tool-name prefix. The three are not
// distinguishable from the value alone, so callers that need to know whether
// a URL was resolved should read ServerURL, which is set only for HTTP/SSE
// servers whose URL the sender actually knew.
//
// HookSource is the reporting agent (claude, codex, cursor, ...), carried so
// callers can attribute provenance coverage per sender rather than as a
// single opaque rate.
type MCPProvenance struct {
	Match      string
	ServerURL  string
	HookSource string
}

// LookupMCPProvenanceByToolCallID returns a map of tool_call_id → the MCP
// provenance the hook recorded for that call. Tool call IDs missing from the
// result had no matching log row: the hook log hasn't landed yet, the sender
// never resolved a server, or — for senders whose recorded tool-call id is not
// the value their trace id derives from — the join cannot succeed at all.
// Callers must treat an absent entry as "unknown provenance", never as "not
// Gram-hosted".
//
// Trace IDs in telemetry_logs are derived from tool call IDs via SHA256
// truncation (see internal/hooks/impl.go), so the lookup is implemented as a
// single CH query against the trace_id bloom-filter index instead of a JSON
// predicate on the tool call ID itself.
//
// since bounds the scan on time_unix_nano, which leads the table's ORDER BY
// and its daily partition key. Without it the trace_id bloom filter (0.01 FPR)
// degrades toward a full 90-day scan once the probe list holds more than a
// handful of ids. A zero value disables the bound.
func (q *Queries) LookupMCPProvenanceByToolCallID(ctx context.Context, projectID uuid.UUID, toolCallIDs []string, since time.Time) (map[string]MCPProvenance, error) {
	if len(toolCallIDs) == 0 {
		return map[string]MCPProvenance{}, nil
	}

	traceToToolCallID := make(map[string]string, len(toolCallIDs))
	traceIDs := make([]string, 0, len(toolCallIDs))
	for _, id := range toolCallIDs {
		if id == "" {
			continue
		}
		traceID := hashToolCallIDToTraceID(id)
		traceToToolCallID[traceID] = id
		traceIDs = append(traceIDs, traceID)
	}
	if len(traceIDs) == 0 {
		return map[string]MCPProvenance{}, nil
	}

	// Admit a row when either attribute is present. Senders that resolve a
	// server from an inventory snapshot write both; Cursor-era rows predating
	// the match attribute carry only the URL, and gating on match alone would
	// silently drop every one of them.
	//
	// A trace can span several rows (PreToolUse, PostToolUse, ...) whose
	// attributes differ — a row written before the sender's inventory snapshot
	// landed carries the degraded tool-name prefix while a sibling carries the
	// real server. Collapse per trace with max() so the populated value always
	// wins over an empty one and the result never depends on row order; the
	// trace_summaries MV resolves the same columns the same way and for the
	// same reason (see clickhouse/schema.sql).
	query := `
		SELECT trace_id,
		       max(toString(attributes.gram.mcp.match)) AS match,
		       max(toString(attributes.gram.mcp.server_url)) AS server_url,
		       max(hook_source) AS hook_source
		FROM telemetry_logs
		WHERE gram_project_id = ?
		  AND trace_id IN ?
		  AND (toString(attributes.gram.mcp.match) != '' OR toString(attributes.gram.mcp.server_url) != '')
	`
	args := []any{projectID, traceIDs}
	if !since.IsZero() {
		query += "\t\t  AND time_unix_nano >= ?\n"
		args = append(args, since.UnixNano())
	}
	query += "\t\tGROUP BY trace_id\n"

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query mcp provenance by trace_id: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]MCPProvenance, len(traceIDs))
	for rows.Next() {
		var traceID, match, serverURL, hookSource string
		if err := rows.Scan(&traceID, &match, &serverURL, &hookSource); err != nil {
			return nil, fmt.Errorf("scan mcp provenance row: %w", err)
		}
		toolCallID, ok := traceToToolCallID[traceID]
		if !ok {
			continue
		}
		// One row per trace: the GROUP BY already collapsed siblings.
		out[toolCallID] = MCPProvenance{
			Match:      match,
			ServerURL:  serverURL,
			HookSource: hookSource,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mcp provenance rows: %w", err)
	}
	return out, nil
}

// hashToolCallIDToTraceID mirrors internal/hooks/impl.go: SHA256 of the
// tool call ID truncated to 16 bytes and hex-encoded yields a
// W3C-compliant 32-char trace ID. Duplicated here (rather than exported
// across packages) because it's a 3-line transform and crossing the
// hooks ↔ telemetry boundary just to share it would invert the
// dependency direction.
func hashToolCallIDToTraceID(toolCallID string) string {
	hash := sha256.Sum256([]byte(toolCallID))
	return hex.EncodeToString(hash[:16])
}
