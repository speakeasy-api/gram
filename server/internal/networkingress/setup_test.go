package networkingress_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: false})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service  *networkingress.Service
	conn     *pgxpool.Pool
	features *productfeatures.Client
	orgID    string
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
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

	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	// The database is cloned per test but Redis is shared across the package,
	// so tests that kept the default organization id would share the
	// entitlement cache — one test toggling the feature would flake its
	// parallel neighbours.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	orgID := "org_" + uuid.NewString()
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Network Ingress Test Org",
		Slug:        "netingress-test-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	authCtx.ActiveOrganizationID = orgID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	auditLogger := audit.NewLogger()
	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)

	svc := networkingress.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, features, testenv.NewEncryptionClient(t), auditLogger)

	ti := &testInstance{
		service:  svc,
		conn:     conn,
		features: features,
		orgID:    orgID,
	}

	// Every method is entitlement-gated, so every test would otherwise 403.
	// Tests that assert the gate itself disable it again.
	productfeaturestest.Enable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureNetworkIngress)

	return ctx, ti
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// adminCtx returns a context granted exactly org:admin, the scope every write
// endpoint requires.
func adminCtx(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))
}

// readCtx returns a context granted exactly org:read, the scope the read
// endpoint requires.
func readCtx(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource))
}

// createAuthKeyIngress is a fixture: a network ingress joined with an auth
// key, created through the real service with default settings.
func createAuthKeyIngress(t *testing.T, ctx context.Context, ti *testInstance) *gen.NetworkIngress {
	t.Helper()

	ingress, err := ti.service.CreateIngress(adminCtx(t, ctx), &gen.CreateIngressPayload{
		SessionToken:       nil,
		Provider:           nil,
		Hostname:           nil,
		Tags:               nil,
		AuthKey:            new("tskey-auth-test-0123456789"),
		OauthClientID:      nil,
		OauthClientSecret:  nil,
		PrivateNetworkOnly: nil,
		IdentityRequired:   nil,
	})
	require.NoError(t, err)
	require.NotNil(t, ingress)

	return ingress
}
