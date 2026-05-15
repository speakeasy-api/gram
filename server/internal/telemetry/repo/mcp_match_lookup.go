package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

// LookupMCPMatchesByToolCallID returns a map of tool_call_id →
// resolved MCP server identifier (the `gram.mcp.match` attribute set by
// the hook on every MCP-routed PreToolUse log). Tool call IDs missing
// from the result either had no associated log row yet or their hook
// log was written before the attribute existed; callers should fall back
// to their own match derivation in that case.
//
// Trace IDs in telemetry_logs are derived from tool call IDs via SHA256
// truncation (see internal/hooks/impl.go), so the lookup is implemented
// as a single CH query against the trace_id bloom-filter index instead
// of a JSON predicate on the tool call ID itself.
func (q *Queries) LookupMCPMatchesByToolCallID(ctx context.Context, projectID uuid.UUID, toolCallIDs []string) (map[string]string, error) {
	if len(toolCallIDs) == 0 {
		return map[string]string{}, nil
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
		return map[string]string{}, nil
	}

	// Select the latest non-empty gram.mcp.match per trace_id. The attribute
	// is set on every MCP-routed PreToolUse log via
	// buildTelemetryAttributesWithMetadata, so any matching row is
	// authoritative for its trace.
	const query = `
		SELECT trace_id, toString(attributes.gram.mcp.match) AS match
		FROM telemetry_logs
		WHERE gram_project_id = ?
		  AND trace_id IN ?
		  AND toString(attributes.gram.mcp.match) != ''
	`
	rows, err := q.conn.Query(ctx, query, projectID, traceIDs)
	if err != nil {
		return nil, fmt.Errorf("query mcp match by trace_id: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string, len(traceIDs))
	for rows.Next() {
		var traceID, match string
		if err := rows.Scan(&traceID, &match); err != nil {
			return nil, fmt.Errorf("scan mcp match row: %w", err)
		}
		toolCallID, ok := traceToToolCallID[traceID]
		if !ok {
			continue
		}
		// First non-empty wins per tool call — the attribute value is
		// expected to be identical across rows for the same trace.
		if _, exists := out[toolCallID]; !exists {
			out[toolCallID] = match
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mcp match rows: %w", err)
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
