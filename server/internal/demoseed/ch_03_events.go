package demoseed

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// chEventsAndPostflight: Agent events, the remaining telemetry, and the postflight checks.
func (s *seeder) chEventsAndPostflight(ctx context.Context, ch driver.Conn) error {
	// TODO(demoseed-go): convert from clickhouse.sql.
	return nil
}
