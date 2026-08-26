package replay

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

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

// newTestGuard returns a Guard namespaced uniquely per test invocation, so
// neither parallel tests nor repeated runs against a reused Redis can inherit
// another run's reservations.
func newTestGuard(t *testing.T, maxHold time.Duration) (*Guard, *redis.Client) {
	t.Helper()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	guard, err := NewRedisGuard(client, string(testenv.NewCacheSuffix(t, "replay")), maxHold)
	require.NoError(t, err)
	return guard, client
}

// testKey returns a Key unique to this test invocation.
func testKey(t *testing.T, id string) Key {
	t.Helper()

	return Key{
		Issuer: t.Name(),
		Client: "https://client.example.com/oauth/client.json",
		ID:     id,
	}
}
