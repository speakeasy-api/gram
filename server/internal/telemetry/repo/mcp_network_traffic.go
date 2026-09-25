package repo

import (
	"context"
	"fmt"
	"time"
)

// MCP network traffic server kinds, matching the server_kind column of
// mcp_network_traffic_hourly_summaries.
const (
	MCPNetworkTrafficServerKindMCP  = "mcp"
	MCPNetworkTrafficServerKindMeta = "meta"
)

type GetMCPNetworkTrafficParams struct {
	GramProjectID string
	ServerKind    string
	ServerID      string
	// From is inclusive and To is exclusive; both are truncated to the hour
	// by the materialized view's bucketing.
	From time.Time
	To   time.Time
}

type MCPNetworkTrafficRow struct {
	Hour         time.Time `ch:"hour"`
	Surface      string    `ch:"surface"`
	RequestCount uint64    `ch:"request_count"`
	LastSeen     time.Time `ch:"last_seen"`
}

// GetMCPNetworkTraffic returns hourly observed request counts per network
// surface for one MCP server or gateway. Rows only exist for hours with
// traffic; callers zero-fill the gaps.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetMCPNetworkTraffic(ctx context.Context, arg GetMCPNetworkTrafficParams) ([]MCPNetworkTrafficRow, error) {
	if arg.ServerID == "" {
		return nil, fmt.Errorf("server id is required")
	}

	rows, err := q.conn.Query(ctx,
		`SELECT hour,
		        toString(surface) AS surface,
		        sum(request_count) AS request_count,
		        max(last_seen) AS last_seen
		 FROM mcp_network_traffic_hourly_summaries
		 WHERE gram_project_id = ?
		   AND server_kind = ?
		   AND server_id = ?
		   AND hour >= ? AND hour < ?
		 GROUP BY hour, surface
		 ORDER BY hour`,
		arg.GramProjectID, arg.ServerKind, arg.ServerID, arg.From.UTC(), arg.To.UTC())
	if err != nil {
		return nil, fmt.Errorf("query mcp network traffic: %w", err)
	}
	defer rows.Close()

	out := []MCPNetworkTrafficRow{}
	for rows.Next() {
		var row MCPNetworkTrafficRow
		if err := rows.ScanStruct(&row); err != nil {
			return nil, fmt.Errorf("scan mcp network traffic row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
