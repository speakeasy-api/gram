package chatanalysis_test

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
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/chatanalysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
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
	service  *chatanalysis.Service
	conn     *pgxpool.Pool
	signaler *captureSignaler
}

// captureSignaler records the project ids the service signals so tests can
// assert the trigger's reach without a Temporal environment.
type captureSignaler struct {
	mu       sync.Mutex
	projects []uuid.UUID
	errors   map[uuid.UUID]error
}

var _ analysis.Signaler = (*captureSignaler)(nil)

func (c *captureSignaler) Signal(_ context.Context, projectID uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.projects = append(c.projects, projectID)
	return c.errors[projectID]
}

func (c *captureSignaler) Signaled() []uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]uuid.UUID, len(c.projects))
	copy(out, c.projects)
	return out
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "chat_analysis_settings_api")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 11)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	signaler := &captureSignaler{projects: nil, errors: nil}

	return ctx, &testInstance{
		service:  chatanalysis.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, audit.NewLogger(), signaler),
		conn:     conn,
		signaler: signaler,
	}
}

// withAdmin returns ctx with the auth context's IsAdmin flag flipped to true.
// Tests for admin-only endpoints opt in explicitly so non-admin paths exercise
// the realistic default produced by authztest.InitAuthContext.
func withAdmin(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	// Copy the struct so flipping the flag never mutates the shared auth
	// context still referenced by the source ctx.
	authCtxCopy := *authCtx
	authCtxCopy.IsAdmin = true
	return contextvalues.SetAuthContext(ctx, &authCtxCopy)
}

func createTargetOrganization(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()
	organizationID := uuid.NewString()
	now := time.Now().UTC()
	err := testrepo.New(ti.conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 organizationID,
		Name:               "Chat Analysis Target",
		Slug:               organizationID,
		GramAccountType:    "free",
		WorkosID:           conv.PtrToPGText(nil),
		FreeTrialStartedAt: conv.ToPGTimestamptz(now),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
		DisabledAt:         conv.PtrToPGTimestamptz(nil),
	})
	require.NoError(t, err)
	return organizationID
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
