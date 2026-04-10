package annotations_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/corpus/annotations"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres: true,
		Redis:    true,
	})
	if err != nil {
		log.Fatalf("launch test infra: %v", err)
		os.Exit(1)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infra: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	svc       *annotations.Service
	conn      *pgxpool.Pool
	projectID uuid.UUID
	orgID     string
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "corpus_annotations")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx := testenv.InitAuthContext(t, t.Context(), conn, sessionManager)

	authctx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authctx.ProjectID)

	svc := annotations.NewService(conn)

	return ctx, &testInstance{
		svc:       svc,
		conn:      conn,
		projectID: *authctx.ProjectID,
		orgID:     authctx.ActiveOrganizationID,
	}
}
