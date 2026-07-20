package chatsessions_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Redis: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

func newTestManager(t *testing.T) *chatsessions.Manager {
	t.Helper()

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	return chatsessions.NewManager(testenv.NewLogger(t), redisClient, "test-jwt-secret")
}
