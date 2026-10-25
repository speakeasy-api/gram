package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// attemptWrites hears each connection attempt batch the telemetry logger
// committed, so a test reads the rows once they exist instead of polling.
type attemptWrites struct {
	batches chan int
}

func (w attemptWrites) OnTelemetryLogsWritten(_ context.Context, params []telemetry.LogParams) {
	if len(params) > 0 && params[0].Attributes[attr.EventURNKey] == mcp.MCPConnectionAttemptEventURN {
		w.batches <- len(params)
	}
}

// watchConnectionAttempts registers before the test sends traffic, as the
// logger requires of observers.
func watchConnectionAttempts(t *testing.T, ti *testInstance) attemptWrites {
	t.Helper()
	writes := attemptWrites{batches: make(chan int, 8)}
	ti.telemLogger.AddObserver(writes)
	return writes
}

// written waits for the next listing's attempt batch and returns its size.
func (w attemptWrites) written(t *testing.T) int {
	t.Helper()
	select {
	case n := <-w.batches:
		return n
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the gateway did not record its listing attempts")
		return 0
	}
}

// connectionAttemptRow is one raw mcp_connection_attempt row of a gateway.
type connectionAttemptRow struct {
	ID           string `ch:"id"`
	ListingID    string `ch:"listing_id"`
	Stage        string `ch:"stage"`
	Outcome      string `ch:"outcome"`
	FailureClass string `ch:"failure_class"`
	Reason       string `ch:"reason"`
	MemberID     string `ch:"mcp_server_id"`
	Subject      string `ch:"subject"`
	ClientKey    string `ch:"client_key"`
	Calls        uint64 `ch:"calls"`
	Retries      uint64 `ch:"retries"`
	EventSource  string `ch:"event_source"`
	GramURN      string `ch:"gram_urn"`
	ToolName     string `ch:"tool_name"`
	TraceID      string `ch:"trace_id"`
	SpanID       string `ch:"span_id"`
	ChatID       string `ch:"chat_id"`
}

// connectionAttempts reads a gateway's committed attempt rows, listing first.
func connectionAttempts(t *testing.T, projectID, gatewayID uuid.UUID) []connectionAttemptRow {
	t.Helper()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	var rows []connectionAttemptRow
	require.NoError(t, chConn.Select(t.Context(), &rows,
		`SELECT toString(attributes.gram.connection_attempt.id) AS id,
			        toString(attributes.gram.connection_attempt.listing_id) AS listing_id,
			        toString(attributes.gram.connection_attempt.stage) AS stage,
			        toString(attributes.gram.connection_attempt.outcome) AS outcome,
			        toString(attributes.gram.connection_attempt.failure_class) AS failure_class,
			        toString(attributes.gram.connection_attempt.reason) AS reason,
			        mcp_server_id,
			        toString(attributes.gram.connection_attempt.subject) AS subject,
			        toString(attributes.gram.connection_attempt.client_key) AS client_key,
			        toUInt64OrZero(toString(attributes.gram.connection_attempt.calls)) AS calls,
			        toUInt64OrZero(toString(attributes.gram.connection_attempt.retries)) AS retries,
			        event_source, gram_urn, tool_name,
			        toString(ifNull(trace_id, '')) AS trace_id,
			        toString(ifNull(span_id, '')) AS span_id,
			        ifNull(gram_chat_id, '') AS chat_id
			 FROM telemetry_logs
			 WHERE gram_project_id = ? AND event_urn = ? AND meta_mcp_server_id = ?
			 ORDER BY stage = 'discovery_listing' DESC, time_unix_nano, id`,
		projectID.String(), mcp.MCPConnectionAttemptEventURN, gatewayID.String()))
	return rows
}

// requireAttemptRowsAreNotUsage checks the properties that keep attempt rows
// out of tool, trace, and chat metrics: only a left-out member carries the
// gateway discovery labels, and no row carries a trace, span, or chat.
func requireAttemptRowsAreNotUsage(t *testing.T, rows []connectionAttemptRow) {
	t.Helper()
	for _, row := range rows {
		require.Empty(t, row.TraceID, "an attempt row never joins a trace")
		require.Empty(t, row.SpanID)
		require.Empty(t, row.ChatID)
		if row.Stage == "discovery_member" && row.Outcome == "failed" {
			require.Equal(t, "meta_discovery", row.EventSource)
			require.Equal(t, "tools/list", row.ToolName)
			continue
		}
		require.Empty(t, row.EventSource)
		require.Empty(t, row.GramURN)
		require.Empty(t, row.ToolName)
	}
}

// requireNoTraceOrToolUsage checks that a project with only gateway listings
// recorded no trace, tool call, or chat request.
func requireNoTraceOrToolUsage(t *testing.T, projectID uuid.UUID) {
	t.Helper()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	var traces uint64
	require.NoError(t, chConn.QueryRow(t.Context(), `SELECT count() FROM trace_summaries WHERE gram_project_id = ?`, projectID.String()).Scan(&traces))
	require.Zero(t, traces)
	var toolCalls, chatRequests uint64
	require.NoError(t, chConn.QueryRow(t.Context(),
		`SELECT countIfMerge(total_tool_calls), countIfMerge(total_chat_requests) FROM metrics_summaries WHERE gram_project_id = ?`,
		projectID.String()).Scan(&toolCalls, &chatRequests))
	require.Zero(t, toolCalls)
	require.Zero(t, chatRequests)
}

// attemptRollup returns a gateway's rolled-up member attempts, retries, and
// recoveries, keyed by outcome and reason.
func attemptRollup(t *testing.T, projectID, gatewayID uuid.UUID) map[string][3]uint64 {
	t.Helper()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	rows, err := chConn.Query(t.Context(),
		`SELECT concat(stage, '/', outcome, '/', reason), sum(attempts), sum(retries), sum(recovered)
		 FROM mcp_connection_attempt_summaries
		 WHERE gram_project_id = toUUID(?) AND server_kind = 'meta' AND server_id = ?
		 GROUP BY stage, outcome, reason`,
		projectID.String(), gatewayID.String())
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	totals := map[string][3]uint64{}
	for rows.Next() {
		var key string
		var attempts, retries, recovered uint64
		require.NoError(t, rows.Scan(&key, &attempts, &retries, &recovered))
		totals[key] = [3]uint64{attempts, retries, recovered}
	}
	require.NoError(t, rows.Err())
	return totals
}

func requireUUIDv7(t *testing.T, value string) {
	t.Helper()
	id, err := uuid.Parse(value)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), id.Version())
}

// memberAttemptsByOutcome groups a listing's member rows by outcome.
func memberAttemptsByOutcome(rows []connectionAttemptRow) map[string][]connectionAttemptRow {
	byOutcome := map[string][]connectionAttemptRow{}
	for _, row := range rows {
		if row.Stage == "discovery_member" {
			byOutcome[row.Outcome] = append(byOutcome[row.Outcome], row)
		}
	}
	return byOutcome
}

// attemptsAfter waits for the next listing's attempt batch, checks its size,
// and reads the gateway's rows.
func attemptsAfter(t *testing.T, writes attemptWrites, projectID, gatewayID uuid.UUID, want int) []connectionAttemptRow {
	t.Helper()
	require.Equal(t, want, writes.written(t))
	rows := connectionAttempts(t, projectID, gatewayID)
	require.Len(t, rows, want)
	return rows
}
