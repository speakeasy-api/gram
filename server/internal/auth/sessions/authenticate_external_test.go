package sessions_test

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/usermanagement"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestAuthenticateSessionCacheFailureIsUnavailable(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "sessionstest")
	require.NoError(t, err)

	redisClient := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() {
		require.NoError(t, redisClient.Close())
	})

	pylonClient, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	idpClient := identity.NewWorkOSAdapter(usermanagement.NewClient("test-api-key"))
	resolver := identity.NewResolver(
		logger,
		tracerProvider,
		cache.NewRedisCacheAdapter(redisClient),
		"",
		"",
		idpClient,
		workos.NewStubClient(),
		orgRepo.New(conn),
		userRepo.New(conn),
		pylonClient,
		posthog.New(t.Context(), logger, "", "", ""),
		testenv.NewGrowthEmitter(t, logger, conn),
		cache.SuffixNone,
	)
	manager := sessions.NewManager(
		logger,
		tracerProvider,
		conn,
		redisClient,
		cache.SuffixNone,
		idpClient,
		billing.NewStubClient(logger, tracerProvider),
		resolver,
	)

	_, err = manager.Authenticate(t.Context(), "session-id")
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnavailable, oopsErr.Code)
	require.NotErrorIs(t, err, redis.Nil)
}
