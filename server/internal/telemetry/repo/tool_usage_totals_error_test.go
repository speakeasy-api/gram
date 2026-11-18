package repo_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// A totals read that ClickHouse stops partway (as it does at its
// max_execution_time or memory limit) must fail, not answer zero calls: the
// zero is shown as the agent having made no calls at all.
func TestGetToolUsageTotals_FailsWhenTheQueryStops(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	projectID := uuid.NewString()
	now := time.Now().UTC().Add(-time.Minute)
	for range 3 {
		traceID := strings.ReplaceAll(uuid.NewString(), "-", "")
		attrs, err := json.Marshal(map[string]any{
			"gram.event.source":         "tool_call",
			"gram.toolset.slug":         "payments",
			"gen_ai.tool.name":          "charge",
			"user.email":                "dev@example.com",
			"http.response.status_code": 200,
		})
		require.NoError(t, err)
		err = queries.InsertTelemetryLogsSync(ctx, []repo.InsertTelemetryLogParams{{
			ID: uuid.NewString(), GramProjectID: projectID,
			TimeUnixNano: now.UnixNano(), ObservedTimeUnixNano: now.UnixNano(),
			TraceID: &traceID, GramURN: "tools:http:gram:charge",
			Body: "hosted tool event", Attributes: string(attrs), ResourceAttributes: "{}",
		}})
		require.NoError(t, err)
	}
	params := repo.GetToolUsageSummaryParams{
		GramProjectID: projectID,
		TimeStart:     now.Add(-time.Hour).UnixNano(),
		TimeEnd:       now.Add(time.Hour).UnixNano(),
	}

	totals, err := queries.GetToolUsageTotals(ctx, params)
	require.NoError(t, err)
	require.Equal(t, uint64(3), totals.EventCount)

	// A group by limit stops the query once it is running and has sent its
	// header, the way a timeout or the memory limit does, rather than
	// rejecting it up front.
	stopped := clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_block_size":         1,
		"max_rows_to_group_by":   1,
		"group_by_overflow_mode": "throw",
	}))
	_, err = queries.GetToolUsageTotals(stopped, params)
	require.Error(t, err)
}
