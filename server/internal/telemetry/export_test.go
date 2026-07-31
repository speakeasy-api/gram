package telemetry

import "context"

// InvalidateForTest drops the cached snapshot for the user so tests can
// simulate TTL expiry deterministically.
func (r *UserInfoResolver) InvalidateForTest(ctx context.Context, organizationID string, userID string) error {
	//nolint:wrapcheck // test-only helper
	return r.cache.DeleteByKey(ctx, userInfoSnapshotCacheKey(organizationID, userID))
}

// WaitForPublishDrains blocks until the drain pool has observed every publish
// result enqueued by PublishLogs so far. Test-only synchronization barrier:
// callers must have already returned from the PublishLogs (or LogBulk) call
// whose drain they await, so the enqueue happens-before this Wait.
func (p *LogPublisher) WaitForPublishDrains() {
	p.drainer.Wait()
}
