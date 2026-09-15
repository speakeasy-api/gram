package openrouter

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	or "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// Each fixture gets a private Redis instance, including repeated invocations
// under -count, while using the production judge rate and burst unchanged.
func testJudgeLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return or.NewJudgeRateLimiter(ratelimit.NewRedisStore(client))
}
