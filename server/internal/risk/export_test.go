package risk

import (
	"context"
	"fmt"
)

// AwaitRealtimeRecords waits for the background recordings of returned scans
// without draining, so tests can read comparison metrics.
func (s *Scanner) AwaitRealtimeRecords(ctx context.Context) error {
	if s.waitRealtimeRecords(ctx) {
		return nil
	}
	return fmt.Errorf("await realtime records: %w", context.Cause(ctx))
}
