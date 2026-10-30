package demoseed

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// runClickHouse regenerates the tenant's telemetry. Rows are built in Go from
// the same helpers as the Postgres seed (chat ids, owners, timestamps), so the
// two stores join exactly.
func (s *seeder) runClickHouse(ctx context.Context, ch driver.Conn) error {
	// lightweight_deletes_sync=2 waits for ALL replicas (prod ClickHouse Cloud
	// is replicated; =1 would let the re-insert race a delete still running on
	// another replica). The base client caps max_execution_time at 60s, which
	// prod-sized telemetry_logs deletes can exceed — raise it here. Inserts are
	// acknowledged: without an explicit posture they would inherit ClickHouse's
	// default async_insert=1 and the server connection's
	// wait_for_async_insert=0, and the postflight checks would race the seed.
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"lightweight_deletes_sync": 2,
		"max_execution_time":       600,
		"async_insert":             1,
		"wait_for_async_insert":    1,
	}))

	phases := []struct {
		name string
		run  func(context.Context, driver.Conn) error
	}{
		{"clickhouse purge and telemetry", s.chPurgeAndTelemetry},
		{"clickhouse detections and findings", s.chDetectionsAndFindings},
		{"clickhouse events and postflight", s.chEventsAndPostflight},
		{"clickhouse sensor readings", s.chSigintReadings},
		{"clickhouse tunnel metrics", s.chTunnelMetrics},
	}
	for _, p := range phases {
		start := time.Now()
		if err := p.run(ctx, ch); err != nil {
			s.logger.ErrorContext(ctx, fmt.Sprintf("demo seed phase %q failed after %s", p.name, time.Since(start).Round(time.Millisecond)), attr.SlogName(p.name), attr.SlogError(err))
			return fmt.Errorf("demo seed phase %q: %w", p.name, err)
		}
		s.logger.InfoContext(ctx, fmt.Sprintf("demo seed phase %q done in %s", p.name, time.Since(start).Round(time.Millisecond)), attr.SlogName(p.name))
	}
	return nil
}
