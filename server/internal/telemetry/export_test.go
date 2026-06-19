package telemetry

import "context"

// InvalidateForTest drops the cached snapshot for the user so tests can
// simulate TTL expiry deterministically.
func (r *UserInfoResolver) InvalidateForTest(ctx context.Context, organizationID string, userID string) error {
	//nolint:wrapcheck // test-only helper
	return r.cache.DeleteByKey(ctx, userInfoSnapshotCacheKey(organizationID, userID))
}
