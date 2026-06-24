package remotemcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newShadowMCPClientForTest constructs a shadowmcp.Client backed by the
// shared test infra (Postgres + Redis from TestMain). The client is
// real-DB but tests that exercise IsEnabledForProject against a fresh
// project see the default "disabled" state because no risk policies
// have been created.
func newShadowMCPClientForTest(t *testing.T) *shadowmcp.Client {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "xmcpshadow")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	accessStore := accesscontrol.NewRedisStore(cacheAdapter, accesscontrol.AlphaTTL)
	return shadowmcp.NewClient(testenv.NewLogger(t), conn, cacheAdapter, accessStore)
}
