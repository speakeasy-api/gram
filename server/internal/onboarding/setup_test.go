package onboarding_test

import (
	"context"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/onboarding"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
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

// fakeEvidence stands in for ClickHouse. Tests set the counts a check should
// see; the hook event count can be narrowed per source.
type fakeEvidence struct {
	mu             sync.Mutex
	hookEvents     map[string]uint64
	anyHookEvents  uint64
	costRows       uint64
	gatewayTraffic uint64
	aiDetections   uint64
}

func (f *fakeEvidence) CountHookEvents(_ context.Context, _ []string, sources []string, _ time.Time) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(sources) == 0 {
		return f.anyHookEvents, nil
	}
	var n uint64
	for _, source := range sources {
		n += f.hookEvents[source]
	}
	return n, nil
}

func (f *fakeEvidence) CountCostRows(context.Context, []string, time.Time) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.costRows, nil
}

func (f *fakeEvidence) CountGatewayTraffic(context.Context, []string, time.Time) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gatewayTraffic, nil
}

func (f *fakeEvidence) CountAIDetections(context.Context, string, time.Time) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.aiDetections, nil
}

func (f *fakeEvidence) seeHookEvents(source string, n uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hookEvents[source] = n
	f.anyHookEvents += n
}

type testInstance struct {
	service  *onboarding.Service
	conn     *pgxpool.Pool
	evidence *fakeEvidence
	orgID    string
	userID   string
}

// newTestService builds the service against a cloned database with the
// catalog synced, and an auth context holding admin grants.
func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("onboarding-"+uuid.NewString()), billing.NewStubClient(logger, tracerProvider))
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	require.NoError(t, onboarding.SyncCatalog(ctx, logger, conn, onboarding.Default))

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	evidence := &fakeEvidence{hookEvents: map[string]uint64{}}
	service := onboarding.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, audit.NewLogger(), evidence)

	return ctx, &testInstance{
		service:  service,
		conn:     conn,
		evidence: evidence,
		orgID:    authCtx.ActiveOrganizationID,
		userID:   authCtx.UserID,
	}
}

// asMember returns a context holding only org:read, which is what membership grants.
func asMember(t *testing.T, ctx context.Context, ti *testInstance) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, ti.orgID))
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareErr *oops.ShareableError
	require.ErrorAs(t, err, &shareErr)
	require.Equal(t, code, shareErr.Code, "error: %v", err)
}
