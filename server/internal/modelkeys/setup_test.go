package modelkeys_test

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	pfrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

// stubProvisioner satisfies openrouter.Provisioner for tests. ProvisionAPIKey
// hands back platformKey; GetKeyUsage returns usageErr so tests can simulate
// the provider rejecting a customer key.
type stubProvisioner struct {
	platformKey string
	usageErr    error
}

var _ openrouter.Provisioner = (*stubProvisioner)(nil)

func (p *stubProvisioner) ProvisionAPIKey(context.Context, string, openrouter.KeyType) (string, error) {
	return p.platformKey, nil
}

func (p *stubProvisioner) RefreshAPIKeyLimit(ctx context.Context, orgID string, keyType openrouter.KeyType, limit *int) (int, error) {
	return 0, nil
}

func (p *stubProvisioner) GetCreditsUsed(ctx context.Context, orgID string, keyType openrouter.KeyType) (float64, int, error) {
	return 0, 0, nil
}

func (p *stubProvisioner) GetKeyUsage(ctx context.Context, apiKey string) (float64, *int64, error) {
	return 0, nil, p.usageErr
}

func (p *stubProvisioner) ReconcileMonthlyCredits(ctx context.Context, orgID string, keyType openrouter.KeyType, currentLimit int64, upstreamLimit *int64) (int64, error) {
	return currentLimit, nil
}

func (p *stubProvisioner) GetModelUsage(ctx context.Context, generationID string, orgID string, keyType openrouter.KeyType) (*openrouter.ModelUsage, error) {
	return nil, errors.New("not implemented")
}

type testInstance struct {
	service     *modelkeys.Service
	conn        *pgxpool.Pool
	enc         *encryption.Client
	provisioner *stubProvisioner
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	return newTestServiceWithRedisDB(t, 0)
}

// newTestServiceWithRedisDB is the variant of [newTestService] that isolates
// the product-feature cache on its own redis database. Every test shares
// mockidp.MockOrgID while cloning its own Postgres database, so a feature
// cached as enabled by one test would leak into a parallel test asserting the
// disabled path.
func newTestServiceWithRedisDB(t *testing.T, redisDB int) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, redisDB)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	enc := testenv.NewEncryptionClient(t)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	provisioner := &stubProvisioner{platformKey: "platform-key", usageErr: nil}

	svc := modelkeys.NewService(
		logger,
		tracerProvider,
		conn,
		sessionManager,
		authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		enc,
		provisioner,
		productfeatures.NewClient(logger, tracerProvider, conn, redisClient),
		audit.NewLogger(),
	)

	return ctx, &testInstance{
		service:     svc,
		conn:        conn,
		enc:         enc,
		provisioner: provisioner,
	}
}

// enableCustomModelKeys turns on the product feature for the auth context's
// organization, which gates the upsert endpoint.
func enableCustomModelKeys(t *testing.T, ctx context.Context, conn *pgxpool.Pool) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	require.NoError(t, pfrepo.New(conn).EnableFeature(ctx, pfrepo.EnableFeatureParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureCustomModelKeys),
	}))
}

func withExactAccessGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "modelkeys-rbac-grants-"+uuid.NewString())
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

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
