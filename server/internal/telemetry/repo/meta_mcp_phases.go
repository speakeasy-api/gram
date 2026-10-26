package repo

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Gateway call phases. A gateway's calls are discovery (finding a member
// tool), execution (calling one), or a Code Mode execute run, which contains
// both kinds of call and counts as neither.
const (
	metaMCPPhaseDiscovery  = "discovery"
	metaMCPPhaseExecution  = "execution"
	metaMCPPhaseExecuteRun = "execute_run"
)

// MetaMCPModeUnrecorded is the mode of a gateway row written before gateway
// rows recorded their connection's discovery mode.
const MetaMCPModeUnrecorded = "unrecorded"

var (
	// metaMCPProgressiveDiscoveryTools are the Progressive drill-down tools.
	// Only Progressive serves them, so a row of one without a recorded mode
	// was still served in Progressive.
	metaMCPProgressiveDiscoveryTools = []string{metamcp.ToolListServers, metamcp.ToolDescribeServer, metamcp.ToolDescribeTools}

	// metaMCPDiscoveryTools are every mode's discovery calls. Other
	// meta_discovery rows, such as connection_status, are neither phase.
	metaMCPDiscoveryTools = slices.Concat(metaMCPProgressiveDiscoveryTools, []string{metamcp.ToolCodeServers, metamcp.ToolCodeSearch, metamcp.ToolCodeDescribe})
)

// The expressions below read one telemetry_logs row. The tool names are
// package constants, so they are written into the SQL as literals.
var (
	// metaMCPPhaseExpr classifies a gateway row as one of the phases above,
	// or '' when it is none of them.
	metaMCPPhaseExpr = "multiIf(" +
		"event_source = 'tool_call', '" + metaMCPPhaseExecution + "', " +
		"event_source = 'meta_discovery' AND tool_name IN (" + chStringList(metaMCPDiscoveryTools) + "), '" + metaMCPPhaseDiscovery + "', " +
		"event_source = 'meta_discovery' AND tool_name = '" + metamcp.ToolCodeExecute + "', '" + metaMCPPhaseExecuteRun + "', " +
		"'')"

	// metaMCPModeExpr is the row's recorded discovery mode, falling back to
	// Progressive for a Progressive drill-down row.
	metaMCPModeExpr = "multiIf(" +
		"ifNull(toString(attributes.gram.meta_mcp_server.discovery_mode), '') != '', toString(attributes.gram.meta_mcp_server.discovery_mode), " +
		"event_source = 'meta_discovery' AND tool_name IN (" + chStringList(metaMCPProgressiveDiscoveryTools) + "), '" + string(metamcp.DiscoveryModeProgressive) + "', " +
		"'" + MetaMCPModeUnrecorded + "')"

	// metaMCPSessionExpr is the session a row belongs to: the client's MCP
	// session, else its chat, else ''.
	metaMCPSessionExpr = "ifNull(nullIf(toString(attributes.mcp.session.id), ''), chat_id)"
)

// chStringList renders constant names as a ClickHouse string list. Only for
// package constants, which never contain quotes.
func chStringList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "'"+value+"'")
	}
	return strings.Join(quoted, ", ")
}

type GetMetaMCPServerPhasesParams struct {
	GramProjectID   string
	MetaMCPServerID string

	// TimeStart and TimeEnd bound the window in Unix nanoseconds, both
	// inclusive.
	TimeStart int64
	TimeEnd   int64

	// IntervalSeconds is the width of each series bucket.
	IntervalSeconds int64
}

// MetaMCPPhaseBucket counts one discovery mode's calls in one series bucket.
type MetaMCPPhaseBucket struct {
	// BucketTimeUnixNano is the bucket start. Buckets start at TimeStart.
	BucketTimeUnixNano int64

	// Mode is the discovery mode, or MetaMCPModeUnrecorded.
	Mode string

	// DiscoveryCalls counts discovery calls.
	DiscoveryCalls uint64

	// ExecutionCalls counts member tool calls.
	ExecutionCalls uint64

	// ExecuteRuns counts Code Mode execute runs.
	ExecuteRuns uint64

	// CallsWithoutSession counts discovery and execution calls with neither an
	// MCP session nor a chat.
	CallsWithoutSession uint64
}

// MetaMCPPositionRow counts one discovery mode's calls at one range of
// positions within their sessions.
type MetaMCPPositionRow struct {
	// Mode is the discovery mode, or MetaMCPModeUnrecorded.
	Mode string `ch:"mode"`

	// Position is the range: 1, 2, 3-5, 6-10, 11-20 or 21+.
	Position string `ch:"position"`

	// DiscoveryCalls counts discovery calls at these positions.
	DiscoveryCalls uint64 `ch:"discovery_calls"`

	// ExecutionCalls counts member tool calls at these positions.
	ExecutionCalls uint64 `ch:"execution_calls"`
}

// MetaMCPPhases is how one gateway's calls divide between discovery and
// execution.
type MetaMCPPhases struct {
	// Series has one row per mode and bucket with calls in the window.
	Series []MetaMCPPhaseBucket

	// Previous has one row per mode with calls in the window of equal length
	// before TimeStart; its bucket times are zero.
	Previous []MetaMCPPhaseBucket

	// Positions has one row per mode and position range with calls whose
	// session is known.
	Positions []MetaMCPPositionRow
}

// GetMetaMCPServerPhases returns one gateway's discovery and execution calls
// by discovery mode: a series over the window, totals for the window before
// it, and counts by position within each session. Reads telemetry_logs:
// session order needs every row's own time and session, which no summary
// keeps, and the bloom filter on meta_mcp_server_id bounds the scan to one
// gateway's rows.
func (q *Queries) GetMetaMCPServerPhases(ctx context.Context, arg GetMetaMCPServerPhasesParams) (MetaMCPPhases, error) {
	phases := MetaMCPPhases{
		Series:    []MetaMCPPhaseBucket{},
		Previous:  []MetaMCPPhaseBucket{},
		Positions: []MetaMCPPositionRow{},
	}
	if arg.MetaMCPServerID == "" {
		return phases, fmt.Errorf("meta mcp server id is required")
	}
	if arg.IntervalSeconds <= 0 || arg.TimeEnd < arg.TimeStart {
		return phases, fmt.Errorf("invalid meta mcp phase window")
	}
	intervalNanos := arg.IntervalSeconds * 1_000_000_000
	previousStart := arg.TimeStart - (arg.TimeEnd - arg.TimeStart)
	// A row at the inclusive end falls in the last bucket that starts inside
	// the window, never in one of its own.
	lastBucket := arg.TimeStart + max(arg.TimeEnd-arg.TimeStart-1, 0)/intervalNanos*intervalNanos

	rows, err := q.conn.Query(ctx,
		`WITH `+metaMCPPhaseExpr+` AS phase,
		      `+metaMCPModeExpr+` AS mode,
		      `+metaMCPSessionExpr+` AS session_key
		 SELECT toUInt8(time_unix_nano < ?) AS previous,
		        if(previous, 0, least(? + intDiv(time_unix_nano - ?, ?) * ?, ?)) AS bucket_time_unix_nano,
		        mode,
		        countIf(phase = '`+metaMCPPhaseDiscovery+`') AS discovery_calls,
		        countIf(phase = '`+metaMCPPhaseExecution+`') AS execution_calls,
		        countIf(phase = '`+metaMCPPhaseExecuteRun+`') AS execute_runs,
		        countIf(phase != '`+metaMCPPhaseExecuteRun+`' AND session_key = '') AS calls_without_session
		 FROM telemetry_logs
		 WHERE gram_project_id = ?
		   AND meta_mcp_server_id = ?
		   AND event_source IN ('tool_call', 'meta_discovery')
		   AND time_unix_nano >= ? AND time_unix_nano <= ?
		   AND phase != ''
		 GROUP BY previous, bucket_time_unix_nano, mode
		 ORDER BY bucket_time_unix_nano, mode`,
		arg.TimeStart, arg.TimeStart, arg.TimeStart, intervalNanos, intervalNanos, lastBucket,
		arg.GramProjectID, arg.MetaMCPServerID, previousStart, arg.TimeEnd)
	if err != nil {
		return phases, fmt.Errorf("query meta mcp phases: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })
	for rows.Next() {
		var previous uint8
		var bucket MetaMCPPhaseBucket
		if err := rows.Scan(&previous, &bucket.BucketTimeUnixNano, &bucket.Mode, &bucket.DiscoveryCalls, &bucket.ExecutionCalls, &bucket.ExecuteRuns, &bucket.CallsWithoutSession); err != nil {
			return phases, fmt.Errorf("scan meta mcp phase row: %w", err)
		}
		if previous == 1 {
			phases.Previous = append(phases.Previous, bucket)
		} else {
			phases.Series = append(phases.Series, bucket)
		}
	}
	if err := rows.Err(); err != nil {
		return phases, fmt.Errorf("read meta mcp phase rows: %w", err)
	}

	// Positions number each session's discovery and execution calls in time
	// order; execute runs contain calls rather than being one.
	positionRows, err := q.conn.Query(ctx,
		`SELECT mode,
		        multiIf(call_position = 1, '1', call_position = 2, '2', call_position <= 5, '3-5',
		                call_position <= 10, '6-10', call_position <= 20, '11-20', '21+') AS position,
		        countIf(phase = '`+metaMCPPhaseDiscovery+`') AS discovery_calls,
		        countIf(phase = '`+metaMCPPhaseExecution+`') AS execution_calls
		 FROM (
		   WITH `+metaMCPPhaseExpr+` AS phase,
		        `+metaMCPModeExpr+` AS mode,
		        `+metaMCPSessionExpr+` AS session_key
		   SELECT phase, mode,
		          row_number() OVER (PARTITION BY session_key ORDER BY time_unix_nano, id) AS call_position
		   FROM telemetry_logs
		   WHERE gram_project_id = ?
		     AND meta_mcp_server_id = ?
		     AND event_source IN ('tool_call', 'meta_discovery')
		     AND time_unix_nano >= ? AND time_unix_nano <= ?
		     AND phase IN ('`+metaMCPPhaseDiscovery+`', '`+metaMCPPhaseExecution+`')
		     AND session_key != ''
		 )
		 GROUP BY mode, position`,
		arg.GramProjectID, arg.MetaMCPServerID, arg.TimeStart, arg.TimeEnd)
	if err != nil {
		return phases, fmt.Errorf("query meta mcp call positions: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return positionRows.Close() })
	for positionRows.Next() {
		var row MetaMCPPositionRow
		if err := positionRows.ScanStruct(&row); err != nil {
			return phases, fmt.Errorf("scan meta mcp call position row: %w", err)
		}
		phases.Positions = append(phases.Positions, row)
	}
	if err := positionRows.Err(); err != nil {
		return phases, fmt.Errorf("read meta mcp call position rows: %w", err)
	}
	return phases, nil
}
