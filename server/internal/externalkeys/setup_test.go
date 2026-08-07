package externalkeys_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	extcred "github.com/speakeasy-api/gram/server/gen/external_credentials"
	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials"
	"github.com/speakeasy-api/gram/server/internal/externalkeys"
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
	service     *externalkeys.Service
	credService *externalcredentials.Service
	conn        *pgxpool.Pool
	features    *productfeatures.Client
	orgID       string
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

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	// The database is cloned per test but Redis is shared across the package, so
	// tests that kept the default organization id would also share the
	// entitlement cache — one test toggling the feature would flake its parallel
	// neighbours. external_keys.organization_id is a foreign key, so the row has
	// to exist.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	orgID := "org_" + uuid.NewString()
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "External Keys Test Org",
		Slug:        "extkeys-test-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	authCtx.ActiveOrganizationID = orgID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	auditLogger := audit.NewLogger()
	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	svc := externalkeys.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, auditLogger, features)
	// A stub resolver, not gcpauth.NewResolver: creating a GCP credential now
	// resolves Gram's own identity to screen the impersonation target, so the real
	// resolver would make this package's fixtures depend on ambient cloud
	// credentials — passing or failing based on whether the developer happens to
	// have gcloud ADC configured.
	credSvc := externalcredentials.NewService(logger, tracerProvider, testenv.NewMeterProvider(t), conn, sessionManager, authzEngine, auditLogger, gcpauth.NewStubResolver(), features, ratelimit.NewRedisStore(redisClient))

	ti := &testInstance{
		service:     svc,
		credService: credSvc,
		conn:        conn,
		features:    features,
		orgID:       orgID,
	}

	// Both the keys service and the credential fixtures below are
	// entitlement-gated, so every test would otherwise 403. Tests that assert the
	// gate itself disable it again.
	productfeaturestest.Enable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	return ctx, ti
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func keyIDs(result *gen.ListExternalKeysResult) []string {
	ids := make([]string, len(result.Keys))
	for i, k := range result.Keys {
		ids[i] = k.ID
	}
	return ids
}

// createAwsIamCredential is a fixture: an AWS IAM credential that can back an
// aws_kms key. Returns its ID.
func createAwsIamCredential(t *testing.T, ctx context.Context, ti *testInstance, name string) string {
	t.Helper()

	cred, err := ti.credService.CreateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &extcred.CreateAwsIamCredentialPayload{
		SessionToken:  nil,
		Name:          name,
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	require.NoError(t, err)
	require.NotNil(t, cred)

	return cred.ID
}

// createGcpIamCredential is a fixture: a GCP IAM credential that can back a
// gcp_kms key. Returns its ID.
func createGcpIamCredential(t *testing.T, ctx context.Context, ti *testInstance, name string) string {
	t.Helper()

	cred, err := ti.credService.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &extcred.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      name,
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
	})
	require.NoError(t, err)
	require.NotNil(t, cred)

	return cred.ID
}

// createAwsKmsKey is a fixture: an aws_kms key backed by the given credential.
func createAwsKmsKey(t *testing.T, ctx context.Context, ti *testInstance, name, credentialID string) *gen.AwsKmsKey {
	t.Helper()

	key, err := ti.service.CreateAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsKmsKeyPayload{
		SessionToken:           nil,
		KeyArn:                 "arn:aws:kms:us-east-1:123456789012:key/" + uuid.NewString(),
		ExternalCredentialID:   credentialID,
		Algorithm:              "RS256",
		Name:                   name,
		CustomerGrantReference: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, key)

	return key
}

// createGcpKmsKey is a fixture: a gcp_kms key backed by the given credential.
func createGcpKmsKey(t *testing.T, ctx context.Context, ti *testInstance, name, credentialID string) *gen.GcpKmsKey {
	t.Helper()

	key, err := ti.service.CreateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpKmsKeyPayload{
		SessionToken:           nil,
		ResourceName:           "projects/gram/locations/global/keyRings/signing/cryptoKeys/" + uuid.NewString() + "/cryptoKeyVersions/1",
		ExternalCredentialID:   credentialID,
		Algorithm:              "ES256",
		Name:                   name,
		CustomerGrantReference: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, key)

	return key
}
