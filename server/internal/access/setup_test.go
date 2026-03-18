package access_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	infra *testenv.Environment
)

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

type testInstance struct {
	service *access.Service
	conn    *pgxpool.Pool
}

// upsertGrant is a test helper that upserts a single grant via the batch API.
func upsertGrant(t *testing.T, ctx context.Context, svc *access.Service, principalUrnStr, scope, resource string) *gen.Grant {
	t.Helper()

	principal, err := urn.ParsePrincipal(principalUrnStr)
	require.NoError(t, err)

	result, err := svc.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		Grants: []*gen.UpsertGrantForm{
			{PrincipalUrn: principal, Scope: scope, Resource: resource},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)

	return result.Grants[0]
}

func newTestAccessService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	svc := access.NewService(logger, conn, sessionManager)

	return ctx, &testInstance{
		service: svc,
		conn:    conn,
	}
}
