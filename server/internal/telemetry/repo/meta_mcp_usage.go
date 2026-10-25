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

	// IntervalSeconds is the width of each discovery failure bucket: a
	// multiple of 30 minutes, aligned to the UTC epoch.
	IntervalSeconds int64
}

// connectionAttemptBucketNanos is the rollup's bucket width.
const connectionAttemptBucketNanos = int64(30 * time.Minute)

type MetaMCPFunnelCounts struct {
	ListServers    uint64
	DescribeServer uint64
	DescribeTools  uint64
	ExecuteTool    uint64
}

type MetaMCPUsage struct {
	Funnel  MetaMCPFunnelCounts
	Members []MetaMCPMemberUsageRow

	// DiscoveryFailures counts failed Direct listing members in exactly
	// [TimeStart, TimeEnd), in ascending UTC-aligned buckets gap-filled with
	// zeros. The first bucket starts at or before TimeStart.
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
// failures over time, and per-member execution breakdown. The funnel and
// breakdown read telemetry_logs: the breakdown needs mcp_server_id, which
// trace_summaries does not carry, and the bloom filter on meta_mcp_server_id
// bounds the scan to one gateway's rows. Discovery failures come from
// connection attempts, see discoveryFailures.
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
	if arg.IntervalSeconds%1800 != 0 {
		return usage, fmt.Errorf("discovery failure interval must be a multiple of 30 minutes")
	}

	rows, err := q.conn.Query(ctx,
		`SELECT tool_name, count() AS calls
		 FROM telemetry_logs
		 WHERE gram_project_id = ?
		   AND meta_mcp_server_id = ?
		   AND event_source = 'meta_discovery'
		   AND tool_name IN ('list_servers', 'describe_server', 'describe_tools')
		   AND time_unix_nano >= ? AND time_unix_nano <= ?
		 GROUP BY tool_name`,
		arg.GramProjectID, arg.MetaMCPServerID, arg.TimeStart, arg.TimeEnd)
	if err != nil {
		return usage, fmt.Errorf("query meta mcp discovery funnel: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var toolName string
		var calls uint64
		if err := rows.Scan(&toolName, &calls); err != nil {
			return usage, fmt.Errorf("scan meta mcp funnel row: %w", err)
		}
		switch toolName {
		case "list_servers":
			usage.Funnel.ListServers += calls
		case "describe_server":
			usage.Funnel.DescribeServer += calls
		case "describe_tools":
			usage.Funnel.DescribeTools += calls
		}
	}
	if err := rows.Err(); err != nil {
		return usage, err
	}

	failures, err := q.discoveryFailures(ctx, arg)
	if err != nil {
		return usage, err
	}
	usage.DiscoveryFailures = failures

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

// discoveryFailures counts failed Direct listing members in exactly
// [TimeStart, TimeEnd). Whole interior half-hours come from the
// mcp_connection_attempt_summaries rollup; the partial half-hour at each edge,
// including the current one, is read from raw rows with the exact bounds. A
// window without a whole half-hour is one raw read. The slices never overlap.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) discoveryFailures(ctx context.Context, arg GetMetaMCPServerUsageParams) ([]MetaMCPDiscoveryFailureBucket, error) {
	intervalNanos := arg.IntervalSeconds * int64(time.Second)
	firstBucket := floorDiv(arg.TimeStart, intervalNanos) * intervalNanos
	interiorStart := -floorDiv(-arg.TimeStart, connectionAttemptBucketNanos) * connectionAttemptBucketNanos
	interiorEnd := floorDiv(arg.TimeEnd, connectionAttemptBucketNanos) * connectionAttemptBucketNanos
	counts := map[int64]uint64{}

	edges := [][2]int64{{arg.TimeStart, arg.TimeEnd}, {0, 0}}
	if interiorStart < interiorEnd {
		edges = [][2]int64{{arg.TimeStart, interiorStart}, {interiorEnd, arg.TimeEnd}}
		rows, err := q.conn.Query(ctx,
			`SELECT toInt64(intDiv(toUnixTimestamp(bucket), ?) * ?) AS interval_start, sum(attempts) AS failed
			 FROM mcp_connection_attempt_summaries
			 WHERE gram_project_id = toUUID(?)
			   AND server_kind = 'meta'
			   AND server_id = ?
			   AND stage = 'discovery_member'
			   AND bucket >= fromUnixTimestamp(?) AND bucket < fromUnixTimestamp(?)
			   AND outcome = 'failed'
			 GROUP BY interval_start`,
			arg.IntervalSeconds, arg.IntervalSeconds, arg.GramProjectID, arg.MetaMCPServerID,
			interiorStart/int64(time.Second), interiorEnd/int64(time.Second))
		if err != nil {
			return nil, fmt.Errorf("query meta mcp discovery failure rollup: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var start int64
			var failed uint64
			if err := rows.Scan(&start, &failed); err != nil {
				return nil, fmt.Errorf("scan meta mcp discovery failure rollup row: %w", err)
			}
			counts[start*int64(time.Second)] += failed
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	rows, err := q.conn.Query(ctx,
		`SELECT toInt64(intDiv(time_unix_nano, ?) * ?) AS interval_start, count() AS failed
		 FROM telemetry_logs
		 WHERE gram_project_id = ?
		   AND ((time_unix_nano >= ? AND time_unix_nano < ?) OR (time_unix_nano >= ? AND time_unix_nano < ?))
		   AND event_urn = 'urn:telemetry:gram_service:log:mcp_connection_attempt'
		   AND meta_mcp_server_id = ?
		   AND toString(attributes.gram.connection_attempt.stage) = 'discovery_member'
		   AND toString(attributes.gram.connection_attempt.outcome) = 'failed'
		 GROUP BY interval_start`,
		intervalNanos, intervalNanos, arg.GramProjectID,
		edges[0][0], edges[0][1], edges[1][0], edges[1][1], arg.MetaMCPServerID)
	if err != nil {
		return nil, fmt.Errorf("query meta mcp discovery failure edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var start int64
		var failed uint64
		if err := rows.Scan(&start, &failed); err != nil {
			return nil, fmt.Errorf("scan meta mcp discovery failure edge row: %w", err)
		}
		counts[start] += failed
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	buckets := []MetaMCPDiscoveryFailureBucket{}
	for bucket := firstBucket; bucket < arg.TimeEnd; bucket += intervalNanos {
		buckets = append(buckets, MetaMCPDiscoveryFailureBucket{BucketTimeUnixNano: bucket, Count: counts[bucket]})
	}
	return buckets, nil
}

// floorDiv divides rounding toward negative infinity.
func floorDiv(a, b int64) int64 {
	quotient := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		quotient--
	}
	return quotient
}
