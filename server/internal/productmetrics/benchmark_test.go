//go:build productmetrics_bench

package productmetrics

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

// TestSyntheticLoad is opt-in because it performs a capacity experiment rather
// than a regression assertion. All records are synthetic and tenant-skewed.
func TestSyntheticLoad(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	now := time.Now().UTC()
	r := NewRepository(conn, func() time.Time { return now })
	const records = 100000
	for _, repetition := range []string{"high", "low"} {
		c := synthetic(Counter)
		c.Definition.Name = "gram.synthetic." + repetition
		c.ObservedAt = now
		start := now.Truncate(time.Hour).Add(-30 * 24 * time.Hour)
		began := time.Now()
		for batch := 0; batch < records/BatchMaxMessages; batch++ {
			rows := make([]Contribution, 0, BatchMaxMessages)
			for j := range BatchMaxMessages {
				i := batch*BatchMaxMessages + j
				row := c
				row.ID = strconv.Itoa(i)
				if i%10 == 0 {
					row.Tenant.OrganizationID = "synthetic-small"
				}
				// High repetition: 100 observations per minute/series. Low:
				// one distinct series per observation, across 30 event-time days.
				series := i / 100
				if repetition == "low" {
					series = i
				}
				row.EventTime = start.Add(time.Duration(series%43200) * time.Minute)
				if i%2 == 0 {
					row.EventTime = now.Truncate(time.Minute).Add(-time.Duration(series%1440+1) * time.Minute)
				}
				row.PointAttributes = []attribute.KeyValue{attribute.String("series", strconv.Itoa(series)), attribute.String("tier", strconv.Itoa(i%2))}
				if i%2 != 0 {
					for n := range 18 {
						row.PointAttributes = append(row.PointAttributes, attribute.String(fmt.Sprintf("dimension.%d", n), "synthetic-value"))
					}
				}
				rows = append(rows, row)
			}
			require.NoError(t, r.Insert(t.Context(), rows))
		}
		elapsed := time.Since(began)
		t.Logf("%s repetition: records=%d elapsed=%s rate=%.0f/s target=1500/s", repetition, records, elapsed, records/elapsed.Seconds())
		require.GreaterOrEqual(t, records/elapsed.Seconds(), float64(1500), "10x peak write proxy")
		for _, days := range []int{1, 30} {
			q := Query{Tenant: c.Tenant, Definition: c.Definition, Start: now.Truncate(time.Minute).Add(-time.Duration(days) * 24 * time.Hour), End: now.Truncate(time.Minute), Interval: 24 * time.Hour, Filters: []Filter{{Dimension: Dimension{Namespace: Point, Key: "tier"}, Value: attribute.StringValue("0")}}, GroupBy: []Dimension{{Namespace: Point, Key: "tier"}}}
			prefix := "synthetic-benchmark-" + uuid.NewString()
			var wg sync.WaitGroup
			var mu sync.Mutex
			durations := make([]time.Duration, 0, 20)
			errors := make(chan error, 20)
			for range 4 {
				wg.Go(func() {
					for range 5 {
						ctx := context.WithValue(t.Context(), queryRequestKey{}, prefix+"-"+uuid.NewString())
						began := time.Now()
						_, err := r.Query(ctx, q)
						elapsed := time.Since(began)
						if err != nil {
							errors <- err
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
			var rows, bytes, memory uint64
			require.NoError(t, conn.QueryRow(t.Context(), "SELECT max(scanned), max(bytes), max(memory) FROM (SELECT log_comment, sum(read_rows) scanned, sum(read_bytes) bytes, max(memory_usage) memory FROM system.query_log WHERE type = 'QueryFinish' AND startsWith(log_comment, ?) GROUP BY log_comment)", prefix).Scan(&rows, &bytes, &memory))
			t.Logf("%s %dd reads concurrency=4 samples=20 p50=%s p95=%s max_scan_rows=%d max_scan_bytes=%d peak_memory=%d", repetition, days, durations[9], durations[18], rows, bytes, memory)
		}
		var parts, rows, bytes uint64
		require.NoError(t, conn.QueryRow(t.Context(), "SELECT count(), sum(rows), sum(bytes_on_disk) FROM system.parts WHERE active AND database=currentDatabase() AND table='product_metric_sums_1m'").Scan(&parts, &rows, &bytes))
		t.Logf("after %s repetition: rollup_rows=%d parts=%d disk_bytes=%d (merges stopped)", repetition, rows, parts, bytes)
	}
}
