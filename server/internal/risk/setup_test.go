package risk_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func testCELEngine(t *testing.T) *celenv.Engine {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	return eng
}

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type signalerStub struct {
	calls []uuid.UUID
}

func (s *signalerStub) Signal(_ context.Context, projectID uuid.UUID) error {
	s.calls = append(s.calls, projectID)
	return nil
}

// syncResultsCleaner implements risk.RiskPolicyResultsCleaner synchronously for
// tests (no Temporal worker available).
type syncResultsCleaner struct {
	conn *pgxpool.Pool
}

func (c *syncResultsCleaner) Clean(ctx context.Context, projectID, policyID uuid.UUID) error {
	if _, err := riskrepo.New(c.conn).DeleteRiskResultsByPolicy(ctx, riskrepo.DeleteRiskResultsByPolicyParams{
		RiskPolicyID: policyID,
		ProjectID:    projectID,
	}); err != nil {
		return fmt.Errorf("delete risk results by policy: %w", err)
	}
	return nil
}

type testInstance struct {
	service        *risk.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
	signaler       *signalerStub
	chatRepo       *chatrepo.Queries
	flags          *feature.InMemory
}

func newTestRiskService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	sig := &signalerStub{}

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	accessStore := accesscontrol.NewRedisStore(cacheAdapter, accesscontrol.AlphaTTL)
	shadowMCPClient := shadowmcp.NewClient(logger, conn, cacheAdapter, accessStore)
	auditLogger := audit.NewLogger()
	flags := &feature.InMemory{}

	svc := risk.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, sig, nil, &syncResultsCleaner{conn: conn}, nil, shadowMCPClient, auditLogger, "test-jwt-secret", nil, nil, flags, testCELEngine(t))

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		signaler:       sig,
		chatRepo:       chatrepo.New(conn),
		flags:          flags,
	}
}

func withExactAccessGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "risk-rbac-grants-"+uuid.NewString())
	for _, grant := range grants {
		selectors, _ := grant.Selector.MarshalJSON()
		_, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   principal,
			Scope:          string(grant.Scope),
			Selectors:      selectors,
		})
		require.NoError(t, err)
	}

	loadedGrants, err := authz.LoadGrants(ctx, conn, authCtx.ActiveOrganizationID, []urn.Principal{principal})
	require.NoError(t, err)

	return authz.GrantsToContext(ctx, loadedGrants)
}
