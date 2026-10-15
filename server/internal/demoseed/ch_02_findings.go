package demoseed

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// chDetectionsAndFindings: Shadow MCP inventory, AI detections and scans, authz challenges, risk findings, skill versions and efficacy, billing meter readings.
func (s *seeder) chDetectionsAndFindings(ctx context.Context, ch driver.Conn) error {
	// TODO(demoseed-go): convert from clickhouse.sql.
	return nil
}
