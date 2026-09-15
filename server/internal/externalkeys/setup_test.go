package externalkeys_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

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
	extcredrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/externalkeys"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
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
	gcpResolver *gcpauth.StubResolver
	kms         *stubSigningClients
	orgID       string
}

// stubSigningClients stands in for the gcpkms.SigningClientFactory the service
// is constructed with. No GCP network path is reachable from a test, so the
// default hands out a real gcpkms.LocalSigningClient: it exercises the same PEM
// parsing, the same provider signature encodings, and the same local
// verification the production client does, against an in-process key.
//
// Verify tests call SetBuild to script the paths a local key cannot produce —
// a KMS error, or a key that signs with an algorithm other than the one recorded.
type stubSigningClients struct {
	mu sync.Mutex

	// build is the current factory behaviour.
	build func(ctx context.Context, tokenSource oauth2.TokenSource) (gcpkms.SigningClient, error)

	// closed counts clients the service handed back. A leaked gRPC connection has
	// no visible symptom in a test, so verify tests assert on this instead.
	closed int
}

func newStubSigningClients(t *testing.T, alg jose.SignatureAlgorithm) *stubSigningClients {
	t.Helper()

	s := &stubSigningClients{mu: sync.Mutex{}, build: nil, closed: 0}
	s.build = func(_ context.Context, _ oauth2.TokenSource) (gcpkms.SigningClient, error) {
		client, err := gcpkms.NewLocalSigningClient(alg)
		if err != nil {
			return nil, fmt.Errorf("build local signing client: %w", err)
		}
		return client, nil
	}

	return s
}

func (s *stubSigningClients) SetBuild(fn func(ctx context.Context, tokenSource oauth2.TokenSource) (gcpkms.SigningClient, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.build = fn
}

// Factory is the gcpkms.SigningClientFactory the service is given.
func (s *stubSigningClients) Factory(ctx context.Context, tokenSource oauth2.TokenSource) (gcpkms.SigningClient, error) {
	s.mu.Lock()
	build := s.build
	s.mu.Unlock()

	client, err := build(ctx, tokenSource)
	if err != nil {
		return nil, err
	}

	return &countingSigningClient{SigningClient: client, clients: s}, nil
}

// Closed reports how many handed-out clients were closed.
func (s *stubSigningClients) Closed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// countingSigningClient records that the service closed the client it was given.
type countingSigningClient struct {
	gcpkms.SigningClient

	clients *stubSigningClients
}

func (c *countingSigningClient) Close() error {
	c.clients.mu.Lock()
	c.clients.closed++
	c.clients.mu.Unlock()

	if err := c.SigningClient.Close(); err != nil {
		return fmt.Errorf("close signing client: %w", err)
	}

	return nil
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

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	auditLogger := audit.NewLogger()
	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	// A stub resolver, not gcpauth.NewResolver: creating a GCP credential resolves
	// Gram's own identity to screen the impersonation target, and so does verify,
	// so the real resolver would make this package depend on ambient cloud
	// credentials — passing or failing based on whether the developer happens to
	// have gcloud ADC configured.
	//
	// Both services share one identity, as they do in production, so a target the
	// credential path accepts is one the verify path accepts too.
	gcpResolver := gcpauth.NewStubResolver()
	gcpIdentity := gcpauth.NewIdentity(gcpResolver)
	// ES256 matches the createGcpKmsKey fixture below, so the default local client
	// signs with the algorithm the fixture key records and verify succeeds.
	kms := newStubSigningClients(t, jose.ES256)

	svc := externalkeys.NewService(logger, tracerProvider, testenv.NewMeterProvider(t), conn, sessionManager, authzEngine, auditLogger, gcpIdentity, kms.Factory, features, ratelimit.NewRedisStore(redisClient))
	credSvc := externalcredentials.NewService(logger, tracerProvider, testenv.NewMeterProvider(t), conn, sessionManager, authzEngine, auditLogger, gcpIdentity, features, ratelimit.NewRedisStore(redisClient))

	ti := &testInstance{
		service:     svc,
		credService: credSvc,
		conn:        conn,
		features:    features,
		gcpResolver: gcpResolver,
		kms:         kms,
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

// createGcpIamCredentialDirect writes an organization GCP credential straight
// through the credentials repo, so verify tests can build the subtype states the
// impersonation-only form can no longer express: a credential naming no identity
// at all, one still carrying Workload Identity Federation columns, or one naming
// a service account inside Gram's own project. Returns its ID.
func createGcpIamCredentialDirect(t *testing.T, ctx context.Context, ti *testInstance, name string, gcpParams extcredrepo.CreateGcpIamCredentialParams) string {
	t.Helper()

	q := extcredrepo.New(ti.conn)

	ec, err := q.CreateExternalCredential(ctx, extcredrepo.CreateExternalCredentialParams{
		OrganizationID: conv.ToPGText(ti.orgID),
		Provider:       "gcp_iam",
		Name:           name,
	})
	require.NoError(t, err)

	gcpParams.ExternalCredentialID = ec.ID
	_, err = q.CreateGcpIamCredential(ctx, gcpParams)
	require.NoError(t, err)

	return ec.ID.String()
}

// softDeleteCredentialDirect tombstones a credential through the repo, skipping
// the service's dependent-key guard. It reproduces the one state the API can no
// longer reach: a key orphaned by a credential deleted before that guard existed.
func softDeleteCredentialDirect(t *testing.T, ctx context.Context, ti *testInstance, credentialID string) {
	t.Helper()

	id, err := uuid.Parse(credentialID)
	require.NoError(t, err)

	_, err = extcredrepo.New(ti.conn).SoftDeleteExternalCredential(ctx, extcredrepo.SoftDeleteExternalCredentialParams{
		ID:             id,
		OrganizationID: conv.ToPGText(ti.orgID),
		Provider:       "gcp_iam",
	})
	require.NoError(t, err)
}

// gramProjectServiceAccount builds a service account address inside the same
// project the stub resolver reports as Gram's own — i.e. exactly what the
// self-project screening must reject. Derived from the stub's constant so the
// tests cannot drift from it.
func gramProjectServiceAccount(name string) string {
	_, domain, _ := strings.Cut(gcpauth.StubResolverPrincipal, "@")
	return name + "@" + domain
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
