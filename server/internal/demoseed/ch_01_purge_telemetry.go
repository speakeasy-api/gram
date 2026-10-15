package demoseed

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// chPurgeAndTelemetry: Scoped deletes of source rows and every MV target, the delete-visibility wait, and the telemetry_logs inserts.
func (s *seeder) chPurgeAndTelemetry(ctx context.Context, ch driver.Conn) error {
	// TODO(demoseed-go): convert from clickhouse.sql.
	return nil
}
