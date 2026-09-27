package telemetry

import "context"

// LogSyncForTest uses the normal logger pipeline but commits the insert before
// returning, so query fixtures do not depend on the shared async insert queue.
func (l *Logger) LogSyncForTest(ctx context.Context, params LogParams) error {
	_, err := l.logBulk(ctx, ctx, []LogParams{params}, true)
	return err
}

// InvalidateForTest drops the cached snapshot for the user so tests can
// simulate TTL expiry deterministically.
func (r *UserInfoResolver) InvalidateForTest(ctx context.Context, organizationID string, userID string) error {
	//nolint:wrapcheck // test-only helper
	return r.cache.DeleteByKey(ctx, userInfoSnapshotCacheKey(organizationID, userID))
}

// WaitForPublishDrains blocks until every ack-drain goroutine spawned by
// PublishLogs so far has finished — i.e. all publish results are resolved and
// the duration metric is recorded. Test-only synchronization barrier: callers
// must have already returned from the PublishLogs (or LogBulk) call whose
// drain they await, so the WaitGroup Add happens-before this Wait.
func (p *LogPublisher) WaitForPublishDrains() {
	p.drains.Wait()
}
