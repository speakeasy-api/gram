package oauth_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
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

func newTokenTestEnv(t *testing.T) *oauthtest.TokenIssuer {
	t.Helper()

	enc := testenv.NewEncryptionClient(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	return oauthtest.NewTokenIssuer(t, cache.NewRedisCacheAdapter(redisClient), enc)
}

func newOAuthServiceTestEnv(t *testing.T) *oauthtest.OAuthServiceEnv {
	t.Helper()

	enc := testenv.NewEncryptionClient(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	return oauthtest.NewOAuthServiceEnv(t, cache.NewRedisCacheAdapter(redisClient), enc)
}
