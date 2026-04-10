package observability_test

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/corpus/observability"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{ClickHouse: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err = cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

func newTestService(t *testing.T) (*observability.Service, context.Context) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	svc := observability.NewService(logger, chConn)
	return svc, ctx
}

// insertSearchEvent inserts a single corpus_search_events row into ClickHouse.
func insertSearchEvent(t *testing.T, ctx context.Context, projectID string, query string, resultCount uint32, latencyMs float64, ts time.Time) {
	t.Helper()

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	filters, err := json.Marshal(map[string]string{})
	require.NoError(t, err)

	err = chConn.Exec(ctx, `
		INSERT INTO corpus_search_events (
			id, project_id, query, filters, result_count, latency_ms,
			session_id, agent, timestamp
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), projectID, query, string(filters), resultCount, latencyMs,
		"", "", ts)
	require.NoError(t, err)
}

func TestSearchLogs_Paginated(t *testing.T) {
	t.Parallel()

	svc, ctx := newTestService(t)

	projectID := uuid.New().String()
	now := time.Now().UTC()

	// Insert 20 search events with distinct timestamps.
	for i := range 20 {
		ts := now.Add(-time.Duration(20-i) * time.Minute)
		insertSearchEvent(t, ctx, projectID, "how to deploy", uint32(i+1), float64(50+i*10), ts)
	}

	// ClickHouse eventual consistency
	time.Sleep(100 * time.Millisecond)

	// Query page 1 with limit=10
	result, err := svc.SearchLogs(ctx, projectID, 10, "")
	require.NoError(t, err)
	require.Len(t, result.Logs, 10)
	require.NotEmpty(t, result.NextCursor, "should have a cursor for next page")

	// Query page 2 with the cursor
	result2, err := svc.SearchLogs(ctx, projectID, 10, result.NextCursor)
	require.NoError(t, err)
	require.Len(t, result2.Logs, 10)
	require.Empty(t, result2.NextCursor, "no more pages")
}

func TestSearchStats_TopQueries(t *testing.T) {
	t.Parallel()

	svc, ctx := newTestService(t)

	projectID := uuid.New().String()
	now := time.Now().UTC()

	// Insert events with repeated queries: "deploy" 5x, "rollback" 3x, "config" 1x
	for i := range 5 {
		insertSearchEvent(t, ctx, projectID, "deploy", 10, 100, now.Add(-time.Duration(i)*time.Minute))
	}
	for i := range 3 {
		insertSearchEvent(t, ctx, projectID, "rollback", 5, 80, now.Add(-time.Duration(i)*time.Minute))
	}
	insertSearchEvent(t, ctx, projectID, "config", 2, 60, now)

	// ClickHouse eventual consistency
	time.Sleep(100 * time.Millisecond)

	stats, err := svc.SearchStats(ctx, projectID)
	require.NoError(t, err)

	// Top queries ranked by frequency
	require.GreaterOrEqual(t, len(stats.TopQueries), 3)
	require.Equal(t, "deploy", stats.TopQueries[0].Query)
	require.Equal(t, uint64(5), stats.TopQueries[0].Count)
	require.Equal(t, "rollback", stats.TopQueries[1].Query)
	require.Equal(t, uint64(3), stats.TopQueries[1].Count)
	require.Equal(t, "config", stats.TopQueries[2].Query)
	require.Equal(t, uint64(1), stats.TopQueries[2].Count)
}

func TestSearchStats_LatencyPercentiles(t *testing.T) {
	t.Parallel()

	svc, ctx := newTestService(t)

	projectID := uuid.New().String()
	now := time.Now().UTC()

	// Insert 100 events with known latencies: 1, 2, 3, ..., 100 ms.
	// p50 = ~50, p95 = ~95, p99 = ~99
	for i := range 100 {
		insertSearchEvent(t, ctx, projectID, "latency-test", 1, float64(i+1), now.Add(-time.Duration(100-i)*time.Second))
	}

	// ClickHouse eventual consistency
	time.Sleep(100 * time.Millisecond)

	stats, err := svc.SearchStats(ctx, projectID)
	require.NoError(t, err)

	// Allow some tolerance for ClickHouse quantile approximation
	require.InDelta(t, 50, stats.LatencyP50, 5, "p50 should be ~50ms")
	require.InDelta(t, 95, stats.LatencyP95, 5, "p95 should be ~95ms")
	require.InDelta(t, 99, stats.LatencyP99, 5, "p99 should be ~99ms")
}
