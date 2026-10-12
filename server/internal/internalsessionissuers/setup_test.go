package internalsessionissuers_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const issuerOrigin = "https://app.example.com"

var infra *testenv.Environment

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
	service  *internalsessionissuers.Service
	conn     *pgxpool.Pool
	features *productfeatures.Client
	flags    *feature.InMemory
	orgID    string
}

// newTestService builds the service for a fresh organization with the
// rollout flag on and the SSO entitlement granted. A nil-provisioner
// deployment passes provisioning=false.
func newTestService(t *testing.T, provisioning bool) (context.Context, *testInstance) {
	t.Helper()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	// Redis is shared across the package, so each test gets its own
	// organization for the entitlement cache and the rate limiter.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: "Issuer Test Org", Slug: "issuer-test-" + uuid.NewString(), WorkosID: conv.ToPGText(orgID), Whitelisted: pgtype.Bool{Bool: false, Valid: false}})
	require.NoError(t, err)
	authCtx.ActiveOrganizationID = orgID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	productfeaturestest.Enable(t, ctx, conn, features, orgID, productfeatures.FeatureSSO)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagInternalSessionIssuer, orgID, true)

	var manager internalsessionissuers.Manager
	if provisioning {
		credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, conn)
		manager = provisiontest.NewProvisioner(t, conn, freshKeys, issuerOrigin, credentialID, "")
	}
	gate := internalsessionissuers.NewGate(conn, features, flags, internalsessionissuers.GateConfig{Provisioning: provisioning, SigningPaused: false})
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	service := internalsessionissuers.NewService(logger, tracerProvider, testenv.NewMeterProvider(t), conn, sessionManager, authzEngine, gate, manager, ratelimit.NewRedisStore(redisClient))
	return ctx, &testInstance{service: service, conn: conn, features: features, flags: flags, orgID: orgID}
}

// freshKeys mints a new in-process key per client, as each KMS key version is.
func freshKeys(context.Context, oauth2.TokenSource) (gcpkms.ProvisioningClient, error) {
	client, err := gcpkms.NewLocalSigningClient(jose.RS256)
	if err != nil {
		return nil, fmt.Errorf("build local signing client: %w", err)
	}
	return client, nil
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func adminCtx(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))
}

func readCtx(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource))
}

func wantIssuer(ti *testInstance) string {
	return issuerOrigin + "/oauth/idp/org/" + ti.orgID
}
