package repo

import (
	"context"
	"fmt"
	"time"
)

type GetMetaMCPServerUsageParams struct {
	GramProjectID   string
	MetaMCPServerID string
	TimeStart       int64
	TimeEnd         int64

	// IntervalSeconds is the width of each discovery failure bucket.
	IntervalSeconds int64
}

type MetaMCPFunnelCounts struct {
	ListServers    uint64
	DescribeServer uint64
	DescribeTools  uint64
	ExecuteTool    uint64
}

type MetaMCPUsage struct {
	Funnel  MetaMCPFunnelCounts
	Members []MetaMCPMemberUsageRow

	// DiscoveryFailures covers [TimeStart, TimeEnd) in ascending order with
	// buckets starting at TimeStart, gap-filled with zeros.
	DiscoveryFailures []MetaMCPDiscoveryFailureBucket
}

type MetaMCPDiscoveryFailureBucket struct {
	BucketTimeUnixNano int64
	Count              uint64
}

type MetaMCPMemberUsageRow struct {
	McpServerID        string `ch:"mcp_server_id"`
	ToolCalls          uint64 `ch:"tool_calls"`
	ErrorCount         uint64 `ch:"error_count"`
	LastCalledUnixNano int64  `ch:"last_called_unix_nano"`
}

// GetMetaMCPServerUsage returns one gateway's discovery funnel, discovery
// failures over time, and per-member execution breakdown. Reads
// telemetry_logs: the member breakdown needs mcp_server_id, which
// trace_summaries does not carry, a tools/list listing writes one row per
// member it left out under a shared trace, which trace_summaries would merge
// into one, and the bloom filter on meta_mcp_server_id bounds the scan to one
// gateway's rows.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetMetaMCPServerUsage(ctx context.Context, arg GetMetaMCPServerUsageParams) (MetaMCPUsage, error) {
	usage := MetaMCPUsage{
		Funnel:            MetaMCPFunnelCounts{ListServers: 0, DescribeServer: 0, DescribeTools: 0, ExecuteTool: 0},
		Members:           []MetaMCPMemberUsageRow{},
		DiscoveryFailures: []MetaMCPDiscoveryFailureBucket{},
	}
	if arg.MetaMCPServerID == "" {
		return usage, fmt.Errorf("meta mcp server id is required")
	}
	if arg.IntervalSeconds <= 0 {
		return usage, fmt.Errorf("discovery failure interval must be positive")
	}
	intervalNanos := arg.IntervalSeconds * int64(time.Second)

	rows, err := q.conn.Query(ctx,
		`SELECT tool_name,
		        ? + intDiv(time_unix_nano - ?, ?) * ? AS bucket_time_unix_nano,
		        count() AS calls,
		        countIf(time_unix_nano < ?) AS calls_before_end
		 FROM telemetry_logs
		 WHERE gram_project_id = ?
		   AND meta_mcp_server_id = ?
		   AND event_source = 'meta_discovery'
		   AND time_unix_nano >= ? AND time_unix_nano <= ?
		 GROUP BY tool_name, bucket_time_unix_nano`,
		arg.TimeStart, arg.TimeStart, intervalNanos, intervalNanos, arg.TimeEnd, arg.GramProjectID, arg.MetaMCPServerID, arg.TimeStart, arg.TimeEnd)
	if err != nil {
		return usage, fmt.Errorf("query meta mcp discovery funnel: %w", err)
	}
	defer rows.Close()
	failures := map[int64]uint64{}
	for rows.Next() {
		var toolName string
		var bucket int64
		var calls, callsBeforeEnd uint64
		if err := rows.Scan(&toolName, &bucket, &calls, &callsBeforeEnd); err != nil {
			return usage, fmt.Errorf("scan meta mcp funnel row: %w", err)
		}
		switch toolName {
		case "list_servers":
			usage.Funnel.ListServers += calls
		case "describe_server":
			usage.Funnel.DescribeServer += calls
		case "describe_tools":
			usage.Funnel.DescribeTools += calls
		case "tools/list":
			// Direct listings only record the members they left out. The series
			// covers [TimeStart, TimeEnd) so adjacent windows never share a row.
			failures[bucket] += callsBeforeEnd
		}
	}
	if err := rows.Err(); err != nil {
		return usage, err
	}
	for bucket := arg.TimeStart; bucket < arg.TimeEnd; bucket += intervalNanos {
		usage.DiscoveryFailures = append(usage.DiscoveryFailures, MetaMCPDiscoveryFailureBucket{BucketTimeUnixNano: bucket, Count: failures[bucket]})
	}

	memberRows, err := q.conn.Query(ctx,
		`SELECT mcp_server_id,
		        count() AS tool_calls,
		        countIf(toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS error_count,
		        max(time_unix_nano) AS last_called_unix_nano
		 FROM telemetry_logs
		 WHERE gram_project_id = ?
		   AND meta_mcp_server_id = ?
		   AND event_source = 'tool_call'
		   AND time_unix_nano >= ? AND time_unix_nano <= ?
		 GROUP BY mcp_server_id
		 ORDER BY tool_calls DESC`,
		arg.GramProjectID, arg.MetaMCPServerID, arg.TimeStart, arg.TimeEnd)
	if err != nil {
		return usage, fmt.Errorf("query meta mcp member usage: %w", err)
	}
	defer memberRows.Close()

	for memberRows.Next() {
		var row MetaMCPMemberUsageRow
		if err := memberRows.ScanStruct(&row); err != nil {
			return usage, fmt.Errorf("scan meta mcp member usage row: %w", err)
		}
		usage.Members = append(usage.Members, row)
	}
	if err := memberRows.Err(); err != nil {
		return usage, err
	}
	for _, m := range usage.Members {
		usage.Funnel.ExecuteTool += m.ToolCalls
	}
	return usage, nil
}
