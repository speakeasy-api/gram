package migration

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// ToolCallWindow is how far back candidate tool calls are counted. A month
// is long enough to tell an idle server from one in daily use.
const ToolCallWindow = 30 * 24 * time.Hour

// ClickHouseToolCalls counts candidate tool calls from telemetry_logs.
type ClickHouseToolCalls struct {
	conn clickhouse.Conn
	now  func() time.Time
}

var _ ToolCallCounter = (*ClickHouseToolCalls)(nil)

// NewClickHouseToolCalls reads tool call counts from conn.
func NewClickHouseToolCalls(conn clickhouse.Conn) *ClickHouseToolCalls {
	return &ClickHouseToolCalls{conn: conn, now: time.Now}
}

type toolsetKey struct {
	projectID uuid.UUID
	slug      string
}

// CountToolCalls counts each candidate's tool calls within ToolCallWindow.
// A call is attributed to the candidate whose canonical wrapper served it, or
// failing that to the candidate with the call's toolset slug in its project.
func (c *ClickHouseToolCalls) CountToolCalls(ctx context.Context, candidates []Candidate) (map[uuid.UUID]uint64, error) {
	counts := make(map[uuid.UUID]uint64, len(candidates))
	if len(candidates) == 0 {
		return counts, nil
	}
	projects := make([]uuid.UUID, 0, len(candidates))
	byServer := make(map[string]uuid.UUID, len(candidates))
	bySlug := make(map[toolsetKey]uuid.UUID, len(candidates))
	seen := map[uuid.UUID]bool{}
	for _, candidate := range candidates {
		row := candidate.Row
		if !seen[row.ProjectID] {
			seen[row.ProjectID] = true
			projects = append(projects, row.ProjectID)
		}
		byServer[row.ToolsetID.String()] = row.ToolsetID
		bySlug[toolsetKey{projectID: row.ProjectID, slug: row.ToolsetSlug}] = row.ToolsetID
	}

	since := c.now().Add(-ToolCallWindow).UnixNano()
	rows, err := c.conn.Query(ctx,
		`SELECT gram_project_id, toolset_slug, mcp_server_id, count() AS calls
		 FROM telemetry_logs
		 WHERE gram_project_id IN ?
		   AND event_source = 'tool_call'
		   AND time_unix_nano >= ?
		 GROUP BY gram_project_id, toolset_slug, mcp_server_id`,
		projects, since)
	if err != nil {
		return nil, fmt.Errorf("query candidate tool calls: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	for rows.Next() {
		var projectID uuid.UUID
		var toolsetSlug, mcpServerID string
		var calls uint64
		if err := rows.Scan(&projectID, &toolsetSlug, &mcpServerID, &calls); err != nil {
			return nil, fmt.Errorf("scan candidate tool calls: %w", err)
		}
		if id, ok := byServer[mcpServerID]; ok {
			counts[id] += calls
			continue
		}
		if mcpServerID != "" {
			continue
		}
		if id, ok := bySlug[toolsetKey{projectID: projectID, slug: toolsetSlug}]; ok {
			counts[id] += calls
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read candidate tool calls: %w", err)
	}
	return counts, nil
}
