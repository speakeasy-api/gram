package explore_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/explore"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: false})
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

type testInstance struct {
	service   *explore.Service
	conn      *pgxpool.Pool
	orgID     string
	projectID uuid.UUID
	userID    string
}

// newTestService builds the service against a cloned database and an auth
// context with a project selected and admin grants.
func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("explore-"+uuid.NewString()), billing.NewStubClient(logger, tracerProvider))
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	service := explore.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, audit.NewLogger())

	return ctx, &testInstance{
		service:   service,
		conn:      conn,
		orgID:     authCtx.ActiveOrganizationID,
		projectID: *authCtx.ProjectID,
		userID:    authCtx.UserID,
	}
}

// asMember returns a context for a different user in the same project who
// holds only project:read, which is what membership grants.
func asMember(t *testing.T, ctx context.Context, ti *testInstance, userID string, extra ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	copied := *authCtx
	copied.UserID = userID
	// auditBase writes ActorDisplayName from Email, so leaving the admin
	// fixture's address here would audit a member's write under the admin.
	memberEmail := userID + "@example.com"
	copied.Email = &memberEmail
	ctx = contextvalues.SetAuthContext(ctx, &copied)
	grants := append([]authz.Grant{authz.NewGrant(authz.ScopeProjectRead, ti.projectID.String())}, extra...)
	return authztest.WithExactGrants(t, ctx, grants...)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareErr *oops.ShareableError
	require.ErrorAs(t, err, &shareErr)
	require.Equal(t, code, shareErr.Code, "error: %v", err)
}
