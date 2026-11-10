package orgencryptionapi_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/org_encryption"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extcredrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/orgencryptionapi"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

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

type testInstance struct {
	service  *orgencryptionapi.Service
	fixture  *orgencryptiontest.Fixture
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

	// A fresh organization per test keeps the shared Redis entitlement cache
	// from leaking between parallel tests.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Organization Encryption API Test Org",
		Slug:        "org-encryption-api-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	authCtx.ActiveOrganizationID = orgID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	fixture := orgencryptiontest.New(t, conn)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)

	service := orgencryptionapi.NewService(logger, tracerProvider, testenv.NewMeterProvider(t), conn, sessionManager, authzEngine, audit.NewLogger(), fixture.Provisioner, features, ratelimit.NewRedisStore(redisClient))

	ti := &testInstance{service: service, fixture: fixture, conn: conn, features: features, orgID: orgID}
	productfeaturestest.Enable(t, ctx, conn, features, orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	return ctx, ti
}

func asAdmin(t *testing.T, ctx context.Context) context.Context {
	t.Helper()

	return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource))
}

func asReader(t *testing.T, ctx context.Context) context.Context {
	t.Helper()

	return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource))
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// provision mints the organization's managed key the way a feature does on
// first use.
func (ti *testInstance) provision(t *testing.T) {
	t.Helper()

	require.NoError(t, ti.fixture.Provisioner.EnsureUseCase(t.Context(), ti.orgID, orgencryption.UseCaseTailscaleGatewayState, orgencryption.SystemActor()))
}

// createCredential writes an organization gcp_iam credential for a customer
// service account and returns its id.
func (ti *testInstance) createCredential(t *testing.T) string {
	t.Helper()

	q := extcredrepo.New(ti.conn)
	ec, err := q.CreateExternalCredential(t.Context(), extcredrepo.CreateExternalCredentialParams{
		OrganizationID: conv.ToPGText(ti.orgID),
		Provider:       "gcp_iam",
		Name:           "customer-" + uuid.NewString(),
	})
	require.NoError(t, err)

	_, err = q.CreateGcpIamCredential(t.Context(), extcredrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      ec.ID,
		ImpersonateServiceAccount: conv.ToPGText("kek-access@customer.iam.gserviceaccount.com"),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
		SkipProjectVerification:   false,
	})
	require.NoError(t, err)

	return ec.ID.String()
}

func (ti *testInstance) createKey(t *testing.T, ctx context.Context, keyID string) *gen.OrgEncryptionKey {
	t.Helper()

	key, err := ti.service.CreateGcpKey(asAdmin(t, ctx), &gen.CreateGcpKeyPayload{
		SessionToken:           nil,
		ExternalCredentialID:   ti.createCredential(t),
		Name:                   "customer key",
		ResourceName:           orgencryptiontest.CustomerKeyName(keyID),
		CustomerGrantReference: nil,
	})
	require.NoError(t, err)

	return key
}
