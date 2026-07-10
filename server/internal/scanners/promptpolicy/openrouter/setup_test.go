package openrouter

import (
	"context"
	"log"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Redis: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

// redisDBCounter hands each test its own Redis logical DB (0-15) so rate-limit
// state from one test never bleeds into another.
var redisDBCounter atomic.Int32

func testJudgeLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()
	client, err := infra.NewRedisClient(t, int(redisDBCounter.Add(1))%16)
	require.NoError(t, err)
	return openrouter.NewJudgeRateLimiter(ratelimit.NewRedisStore(client))
}
