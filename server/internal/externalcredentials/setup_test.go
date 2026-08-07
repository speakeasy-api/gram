package externalcredentials_test

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
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
	service     *externalcredentials.Service
	conn        *pgxpool.Pool
	gcpResolver *gcpauth.StubResolver
	features    *productfeatures.Client
	orgID       string
}

// withAdmin returns ctx with the auth context's IsAdmin flag flipped to true.
// Admin-only endpoints opt in explicitly so non-admin paths exercise the
// realistic default produced by authztest.InitAuthContext.
func withAdmin(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.IsAdmin = true
	return contextvalues.SetAuthContext(ctx, authCtx)
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

	// The database is cloned per test but Redis is shared across the package, so
	// tests that kept the default organization id would also share the
	// entitlement cache and the verify rate-limit bucket — one test disabling the
	// feature or draining the bucket would then flake its parallel neighbours.
	// A per-test organization keeps both per-test. external_credentials.
	// organization_id is a foreign key, so the row has to exist.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	orgID := "org_" + uuid.NewString()
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "External Credentials Test Org",
		Slug:        "extcred-test-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	authCtx.ActiveOrganizationID = orgID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	auditLogger := audit.NewLogger()
	// Verify tests call SetResolve to exercise the failure and unsupported-mode
	// paths; the default answers impersonation and ambient offline.
	gcpResolver := gcpauth.NewStubResolver()
	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	svc := externalcredentials.NewService(logger, tracerProvider, testenv.NewMeterProvider(t), conn, sessionManager, authzEngine, auditLogger, gcpResolver, features, ratelimit.NewRedisStore(redisClient))

	ti := &testInstance{
		service:     svc,
		conn:        conn,
		gcpResolver: gcpResolver,
		features:    features,
		orgID:       authCtx.ActiveOrganizationID,
	}

	// The organization tier is entitlement-gated, so every org-scoped test would
	// otherwise 403. Tests that assert the gate itself disable it again.
	productfeaturestest.Enable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	return ctx, ti
}

// gramProjectServiceAccount builds a service account address inside the same
// project the stub resolver reports as Gram's own — i.e. exactly what the
// self-project screening must reject. Derived from the stub's constant so the
// tests cannot drift from it.
func gramProjectServiceAccount(name string) string {
	_, domain, _ := strings.Cut(gcpauth.StubResolverPrincipal, "@")
	return name + "@" + domain
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func credentialIDs(result *gen.ListExternalCredentialsResult) []string {
	ids := make([]string, len(result.Credentials))
	for i, c := range result.Credentials {
		ids[i] = c.ID
	}
	return ids
}

// createAWSExternalIDCredential is a fixture: an AWS credential that assumes a
// role with a Gram-generated ExternalId.
func createAWSExternalIDCredential(t *testing.T, ctx context.Context, ti *testInstance, name string) *gen.AwsIamCredential {
	t.Helper()

	cred, err := ti.service.CreateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsIamCredentialPayload{
		SessionToken:  nil,
		Name:          name,
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	require.NoError(t, err)
	require.NotNil(t, cred)

	return cred
}

// createGCPImpersonationCredential is a fixture: a GCP credential that
// impersonates a service account.
func createGCPImpersonationCredential(t *testing.T, ctx context.Context, ti *testInstance, name string) *gen.GcpIamCredential {
	t.Helper()

	cred, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      name,
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
	})
	require.NoError(t, err)
	require.NotNil(t, cred)

	return cred
}

// createGCPWifCredentialDirect is a fixture for a state the API can no longer
// produce: an organization GCP credential carrying Workload Identity Federation
// columns, as rows created before this tier became impersonation-only do.
func createGCPWifCredentialDirect(t *testing.T, ctx context.Context, ti *testInstance, name string) *gen.GcpIamCredential {
	t.Helper()

	return createGCPCredentialDirect(t, ctx, ti, name, repo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      uuid.Nil,
		ImpersonateServiceAccount: conv.ToPGText("hop@customer.iam.gserviceaccount.com"),
		WifPoolID:                 conv.ToPGText("gram-pool"),
		WifProviderID:             conv.ToPGText("gram-provider"),
		WifProjectNumber:          conv.ToPGText("123456789012"),
	})
}

// createGCPAmbientCredentialDirect is a fixture for the other state the API can
// no longer produce: an organization GCP credential naming no identity at all,
// which the resolver would treat as Gram's own ambient one.
func createGCPAmbientCredentialDirect(t *testing.T, ctx context.Context, ti *testInstance, name string) *gen.GcpIamCredential {
	t.Helper()

	return createGCPCredentialDirect(t, ctx, ti, name, repo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      uuid.Nil,
		ImpersonateServiceAccount: pgtype.Text{String: "", Valid: false},
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
	})
}

// createGCPCredentialDirect writes an organization GCP credential straight
// through the repo, so tests can build subtype states the impersonation-only
// form can no longer express. The caller's ExternalCredentialID is ignored — the
// supertype row is created here and its id is used.
func createGCPCredentialDirect(t *testing.T, ctx context.Context, ti *testInstance, name string, gcpParams repo.CreateGcpIamCredentialParams) *gen.GcpIamCredential {
	t.Helper()

	q := repo.New(ti.conn)

	ec, err := q.CreateExternalCredential(ctx, repo.CreateExternalCredentialParams{
		OrganizationID: conv.ToPGText(ti.orgID),
		Provider:       "gcp_iam",
		Name:           name,
	})
	require.NoError(t, err)

	gcpParams.ExternalCredentialID = ec.ID
	gcp, err := q.CreateGcpIamCredential(ctx, gcpParams)
	require.NoError(t, err)

	return mv.BuildGcpIamCredentialView(ec, gcp)
}

// createPlatformGCPAmbientCredential is a fixture: a platform (organization_id
// NULL, project_id NULL) GCP credential using the ambient attached identity.
func createPlatformGCPAmbientCredential(t *testing.T, ctx context.Context, ti *testInstance, name string) *adminecgen.GcpIamCredential {
	t.Helper()

	cred, err := ti.service.CreateGcpIamPlatformCredential(withAdmin(t, ctx), &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      name,
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	require.NoError(t, err)
	require.NotNil(t, cred)

	return cred
}
