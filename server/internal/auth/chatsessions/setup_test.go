package chatsessions_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
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

func newTestManager(t *testing.T) (*chatsessions.Manager, *sessions.Manager) {
	t.Helper()

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	sessionManager := sessions.NewManager(logger, testenv.NewTracerProvider(t), nil, redisClient, cache.SuffixNone, nil, nil, nil)
	return chatsessions.NewManager(logger, redisClient, "test-jwt-secret", sessionManager), sessionManager
}
