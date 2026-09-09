package openrouter

import (
	"context"
	"github.com/google/uuid"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

// Each fixture gets a unique bucket namespace, including repeated invocations
// under -count. Cycling through Redis's 16 logical DBs reuses drained buckets.
func testJudgeLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()
	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	// Match NewJudgeRateLimiter's rate and burst, but isolate this fixture.
	return ratelimit.New(ratelimit.NewRedisStore(client), "test-judge-"+uuid.NewString(), ratelimit.PerMinute(250).WithBurst(50))
}
