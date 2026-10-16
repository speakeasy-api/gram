package dashboards_test

import (
	"context"
	"log"
	"os"
	"sync/atomic"
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
	"github.com/speakeasy-api/gram/server/internal/dashboards"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
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
	service   *dashboards.Service
	features  *testFeatures
	conn      *pgxpool.Pool
	orgID     string
	projectID uuid.UUID
	userID    string
}

// testFeatures is a per-test product feature set: whether the organization
// has signals intelligence, which the sensor datasets need. The real client
// caches in a Redis every test shares, under one fixed organization.
type testFeatures struct{ signalsIntelligence atomic.Bool }

func (f *testFeatures) IsFeatureEnabled(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	return feature == productfeatures.FeatureSignalsIntelligence && f.signalsIntelligence.Load(), nil
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

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("dashboards-"+uuid.NewString()), billing.NewStubClient(logger, tracerProvider))
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	features := &testFeatures{}
	catalogs, err := analytics.NewEngine(conn, nil, features)
	require.NoError(t, err)
	service := dashboards.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, audit.NewLogger(), catalogs)

	return ctx, &testInstance{
		service:   service,
		features:  features,
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
