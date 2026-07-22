package externalkeys_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	extcred "github.com/speakeasy-api/gram/server/gen/external_credentials"
	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials"
	"github.com/speakeasy-api/gram/server/internal/externalkeys"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	svc := externalkeys.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, auditLogger)
	credSvc := externalcredentials.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, auditLogger)

	return ctx, &testInstance{service: svc, credService: credSvc, conn: conn}
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
		ImpersonateServiceAccount: new("gram@customer.iam.gserviceaccount.com"),
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
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
