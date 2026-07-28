package externalcredentials_test

import (
	"context"
	"log"
	"os"
	"testing"

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
	"github.com/speakeasy-api/gram/server/internal/externalcredentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	gcpResolver *fakeGcpResolver
}

// fakeGcpResolver is a hand-rolled stand-in for gcpauth.Resolver (our own
// interface). Tests set fn to control the "who am I" probe outcome.
type fakeGcpResolver struct {
	fn func(ctx context.Context, cred gcpauth.Credential) (gcpauth.Principal, error)
}

func (f *fakeGcpResolver) ResolvePrincipal(ctx context.Context, cred gcpauth.Credential) (gcpauth.Principal, error) {
	return f.fn(ctx, cred)
}

// withAdmin returns ctx with the auth context's IsAdmin flag flipped to true.
// Admin-only endpoints opt in explicitly so non-admin paths exercise the
// realistic default produced by testenv.InitAuthContext.
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

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	auditLogger := audit.NewLogger()
	// Default probe: a successful in-cluster ambient resolution. Verify tests
	// override fn to exercise the failure and unsupported-mode paths.
	gcpResolver := &fakeGcpResolver{fn: func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{Email: "gram@gram-platform.iam.gserviceaccount.com", Source: gcpauth.SourceMetadataServer}, nil
	}}
	svc := externalcredentials.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, auditLogger, gcpResolver)

	return ctx, &testInstance{service: svc, conn: conn, gcpResolver: gcpResolver}
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
		ImpersonateServiceAccount: new("gram@customer.iam.gserviceaccount.com"),
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	require.NoError(t, err)
	require.NotNil(t, cred)

	return cred
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
