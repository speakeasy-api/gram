package risk_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"sync"
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
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func testCELEngine(t *testing.T) *celenv.Engine {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	return eng
}

func testPresetLibrary(t *testing.T) *presetlib.Library {
	t.Helper()
	lib, err := presetlib.New()
	require.NoError(t, err)
	return lib
}

func newTestCustomRuleAnalyzer(t *testing.T, conn riskrepo.DBTX) *customruleanalyzer.Scanner {
	t.Helper()
	scanner, err := customruleanalyzer.NewScanner(conn)
	require.NoError(t, err)

	return scanner
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
	mu    sync.Mutex
	calls []uuid.UUID
}

type countingCache struct {
	cache.Cache
	mu      sync.Mutex
	deletes []string
}

func (c *countingCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	c.deletes = append(c.deletes, key)
	c.mu.Unlock()
	if err := c.Cache.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete counted cache key: %w", err)
	}
	return nil
}

func (c *countingCache) DeleteCountContaining(fragment string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, key := range c.deletes {
		if strings.Contains(key, fragment) {
			count++
		}
	}
	return count
}

func (s *signalerStub) Signal(_ context.Context, projectID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, projectID)
	return nil
}

func (s *signalerStub) Calls() []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
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

// stubJudge is a settable promptpolicy.Evaluator for eval-endpoint tests. When evaluate
// is nil it never matches; otherwise it delegates so a test can flag messages by
// content.
type stubJudge struct {
	evaluate func(in promptpolicy.Input) (*promptpolicy.Verdict, error)
}

func (s *stubJudge) Evaluate(_ context.Context, in promptpolicy.Input) (*promptpolicy.Verdict, error) {
	if s.evaluate == nil {
		return nil, nil
	}
	return s.evaluate(in)
}

type testInstance struct {
	service                      *risk.Service
	conn                         *pgxpool.Pool
	sessionManager               *sessions.Manager
	signaler                     *signalerStub
	chatRepo                     *chatrepo.Queries
	flags                        *feature.InMemory
	cacheAdapter                 cache.Cache
	judge                        *stubJudge
	reconcileShadowMCPPolicyURLs risk.ShadowMCPPolicyURLReconciler
	shadowMCPInventoryURLLookup  risk.ShadowMCPInventoryURLLookup
	completionClient             openrouter.CompletionClient
	cacheDeletes                 *countingCache
}

func newTestRiskService(t *testing.T, configure ...func(*testInstance)) (context.Context, *testInstance) {
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

	cacheAdapter := &countingCache{Cache: cache.NewRedisCacheAdapter(redisClient), mu: sync.Mutex{}, deletes: nil}
	accessStore := accesscontrol.NewRedisStore(cacheAdapter, accesscontrol.AlphaTTL)
	shadowMCPClient := shadowmcp.NewClient(logger, conn, cacheAdapter, accessStore, nil)
	auditLogger := audit.NewLogger()
	flags := &feature.InMemory{}

	judge := &stubJudge{evaluate: nil}

	ti := &testInstance{
		service:                      nil,
		conn:                         conn,
		sessionManager:               sessionManager,
		signaler:                     sig,
		chatRepo:                     chatrepo.New(conn),
		flags:                        flags,
		cacheAdapter:                 cacheAdapter,
		judge:                        judge,
		reconcileShadowMCPPolicyURLs: policybypass.ReconcilePolicyURLs,
		shadowMCPInventoryURLLookup: func(_ context.Context, _ uuid.UUID, canonicalURLs []string) ([]string, error) {
			return canonicalURLs, nil
		},
		completionClient: nil,
		cacheDeletes:     cacheAdapter,
	}
	for _, configureInstance := range configure {
		configureInstance(ti)
	}
	ti.service = risk.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, sig, nil, &syncResultsCleaner{conn: conn}, ti.completionClient, shadowMCPClient, auditLogger, cacheAdapter, "test-jwt-secret", nil, nil, flags, testCELEngine(t), testPresetLibrary(t), judge.Evaluate, func(ctx context.Context, db riskrepo.DBTX, input policybypass.ReconcilePolicyURLsInput) error {
		return ti.reconcileShadowMCPPolicyURLs(ctx, db, input)
	}, func(ctx context.Context, projectID uuid.UUID, canonicalURLs []string) ([]string, error) {
		return ti.shadowMCPInventoryURLLookup(ctx, projectID, canonicalURLs)
	})

	return ctx, ti
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

func shadowMCPPolicyAllowedURLs(t *testing.T, ctx context.Context, conn *pgxpool.Pool, policyID string) []string {
	t.Helper()

	principals := shadowMCPPolicyURLPrincipals(t, ctx, conn, policyID)
	urls := make([]string, 0, len(principals))
	for serverURL := range principals {
		urls = append(urls, serverURL)
	}
	slices.Sort(urls)
	return urls
}

func shadowMCPPolicyURLPrincipals(t *testing.T, ctx context.Context, conn *pgxpool.Pool, policyID string) map[string][]string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	grants, err := authz.ListGrantsForResource(ctx, conn, authz.Resource{
		OrganizationID: authCtx.ActiveOrganizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID,
	})
	require.NoError(t, err)

	result := make(map[string][]string)
	for _, grant := range grants {
		if grant.Effect != authz.PolicyEffectAllow {
			continue
		}
		serverURL := grant.Selector[authz.SelectorKeyServerURL]
		if serverURL == "" {
			continue
		}
		result[serverURL] = append(result[serverURL], grant.PrincipalUrn)
	}
	for serverURL := range result {
		slices.Sort(result[serverURL])
		result[serverURL] = slices.Compact(result[serverURL])
	}
	return result
}

func riskPolicyExistsByName(t *testing.T, ctx context.Context, conn *pgxpool.Pool, name string) bool {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)
	policies, err := riskrepo.New(conn).ListRiskPolicies(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	for _, policy := range policies {
		if policy.Name == name {
			return true
		}
	}
	return false
}
