package internalsessionissuers_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/audit"
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
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
	gate    *internalsessionissuers.Gate
	service *internalsessionissuers.Service
	conn    *pgxpool.Pool
	flags   *feature.InMemory
	orgID   string
	// providersFail makes every flag lookup error.
	providersFail *atomic.Bool
	// flagsStaleFor is how long ago the flag definitions were last current.
	flagsStaleFor *atomic.Int64
}

type failingFlags struct {
	*feature.InMemory
	fail *atomic.Bool
	// staleFor ages the flag definitions, as a provider serving its cache
	// through an outage would.
	staleFor *atomic.Int64
}

func (f failingFlags) FlagDefinitionsConfirmedAt() (time.Time, bool) {
	return time.Now().Add(-time.Duration(f.staleFor.Load())), true
}

var errProviderDown = errors.New("provider unavailable")

func (f failingFlags) IsFlagEnabled(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (bool, error) {
	if f.fail.Load() {
		return false, errProviderDown
	}
	return f.InMemory.IsFlagEnabled(ctx, flag, distinctID, groups) //nolint:wrapcheck // Test double.
}

func (f failingFlags) EvaluateFlag(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
	if f.fail.Load() {
		return feature.EvaluationIndeterminate, errProviderDown
	}
	return f.InMemory.EvaluateFlag(ctx, flag, distinctID, groups) //nolint:wrapcheck // Test double.
}

// newTestService builds the service for a fresh organization with the
// rollout flag on and a verified domain. A nil-provisioner
// deployment passes provisioning=false.
func newTestService(t *testing.T, provisioning bool) (context.Context, *testInstance) {
	t.Helper()
	return newTestServiceWithMintStore(t, provisioning, false)
}

// newTestServiceWithMintStore can give the mint limiter a closed Redis client.
func newTestServiceWithMintStore(t *testing.T, provisioning, mintStoreDown bool) (context.Context, *testInstance) {
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
	// organization for the rate limiter.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: "Issuer Test Org", Slug: "issuer-test-" + uuid.NewString(), WorkosID: conv.ToPGText(orgID), Whitelisted: pgtype.Bool{Bool: false, Valid: false}})
	require.NoError(t, err)
	authCtx.ActiveOrganizationID = orgID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	require.NoError(t, testrepo.New(conn).SetOrganizationVerifiedDomainsFixture(ctx, testrepo.SetOrganizationVerifiedDomainsFixtureParams{ID: orgID, VerifiedDomains: []string{"example.com"}}))
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagInternalSessionIssuer, orgID, true)
	flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, orgID, false)
	providersFail := &atomic.Bool{}
	flagsStaleFor := &atomic.Int64{}

	var manager internalsessionissuers.Manager
	if provisioning {
		credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, conn)
		manager = provisiontest.NewProvisioner(t, conn, freshKeys, issuerOrigin, credentialID, "")
	}
	gate := internalsessionissuers.NewGate(conn, failingFlags{InMemory: flags, fail: providersFail, staleFor: flagsStaleFor}, internalsessionissuers.GateConfig{Provisioning: provisioning, SigningPaused: false})
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	mintStore := ratelimit.NewRedisStore(redisClient)
	if mintStoreDown {
		// Nothing listens on port 1, so every limiter call fails.
		down := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
		t.Cleanup(func() { _ = down.Close() })
		mintStore = ratelimit.NewRedisStore(down)
	}
	service := internalsessionissuers.NewService(logger, tracerProvider, testenv.NewMeterProvider(t), conn, sessionManager, authzEngine, audit.NewLogger(), gate, manager, mintStore)
	return ctx, &testInstance{gate: gate, service: service, conn: conn, flags: flags, orgID: orgID, providersFail: providersFail, flagsStaleFor: flagsStaleFor}
}

// provisionIssuer creates the organization's issuer outside the service, for
// deployments whose service has no manager.
func provisionIssuer(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	_, err := provisiontest.NewProvisioner(t, ti.conn, freshKeys, issuerOrigin, credentialID, "").EnsureEMAIssuer(ctx, ti.orgID)
	require.NoError(t, err)
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

// setVerifiedDomains replaces the organization's verified domains.
func setVerifiedDomains(t *testing.T, ctx context.Context, ti *testInstance, domains ...string) {
	t.Helper()
	require.NoError(t, testrepo.New(ti.conn).SetOrganizationVerifiedDomainsFixture(ctx, testrepo.SetOrganizationVerifiedDomainsFixtureParams{ID: ti.orgID, VerifiedDomains: append([]string{}, domains...)}))
}
