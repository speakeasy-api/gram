//go:build productmetrics_bench

package productmetrics

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

// TestSeededSeriesQueries measures the complete Go query path, including active
// series discovery, catalogue integrity checks, filtering, and aggregate reads.
func TestSeededSeriesQueries(t *testing.T) { //nolint:paralleltest // exclusive capacity experiment on a preseeded database
	conn := seededBenchmarkClickhouse(t)
	now := time.Now().UTC()
	r := NewRepository(conn, func() time.Time { return now })
	for _, days := range []int{1, 30, 90} {
		for _, instrument := range []Instrument{Counter, Histogram} {
			for _, grouped := range []bool{false, true} {
				q := Query{Tenant: Tenant{OrganizationID: "synthetic-large", ProjectID: uuid.MustParse("00000000-0000-4000-8000-000000000001")}, Definition: Definition{ScopeName: "gram.synthetic", ScopeVersion: "1", Name: "gram.synthetic.requests", Unit: "{request}", Instrument: instrument}, Start: now.Truncate(time.Minute).Add(-time.Duration(days) * 24 * time.Hour), End: now.Truncate(time.Minute), Interval: 5 * time.Minute, Filters: []Filter{{Dimension: Dimension{Namespace: Point, Key: "model"}, Value: attribute.StringValue("model-0")}, {Dimension: Dimension{Namespace: Resource, Key: "deployment.environment.name"}, Value: attribute.StringValue("production")}}}
				if instrument == Histogram {
					q.Definition.Name = "gram.synthetic.duration"
					q.Definition.Unit = "s"
				}
				if days == 30 {
					q.Interval = time.Hour
				}
				if days == 90 {
					q.Interval = 24 * time.Hour
					q.Start = EarliestBucket(now)
				}
				if grouped {
					q.GroupBy = []Dimension{{Namespace: Resource, Key: "cloud.region"}}
				}
				expected, err := r.Query(t.Context(), q)
				if err != nil {
					t.Logf("REJECTED instrument=%s days=%d grouped=%t err=%v", instrument, days, grouped, err)
					continue
				}
				require.NotEmpty(t, expected)
				for _, concurrency := range []int{1, 4} {
					prefix := "series-capacity-" + uuid.NewString()
					var wg sync.WaitGroup
					var mu sync.Mutex
					durations := make([]time.Duration, 0, 20)
					errors := make(chan error, 20)
					for range concurrency {
						wg.Go(func() {
							for range 20 / concurrency {
								ctx := clickhouse.Context(t.Context(), clickhouse.WithQueryID(prefix+"-"+uuid.NewString()))
								began := time.Now()
								points, err := r.Query(ctx, q)
								elapsed := time.Since(began)
								if err != nil {
									errors <- err
								} else if len(points) != len(expected) {
									errors <- fmt.Errorf("unstable result cardinality")
								}
								mu.Lock()
								durations = append(durations, elapsed)
								mu.Unlock()
							}
						})
					}
					wg.Wait()
					close(errors)
					for err := range errors {
						require.NoError(t, err)
					}
					slices.Sort(durations)
					require.NoError(t, conn.Exec(t.Context(), "SYSTEM FLUSH LOGS"))
					var scans, bytes, memory uint64
					require.NoError(t, conn.QueryRow(t.Context(), `SELECT max(scanned),max(bytes),max(memory) FROM (SELECT query_id,sum(read_rows) scanned,sum(read_bytes) bytes,max(memory_usage) memory FROM system.query_log WHERE type='QueryFinish' AND startsWith(query_id,?) GROUP BY query_id)`, prefix).Scan(&scans, &bytes, &memory))
					t.Logf("instrument=%s days=%d grouped=%t concurrency=%d rows=%d p50=%s p95=%s scan_rows=%d scan_bytes=%d peak_phase_memory=%d", instrument, days, grouped, concurrency, len(expected), durations[9], durations[18], scans, bytes, memory)
				}
			}
		}
	}
}
