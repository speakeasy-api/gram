package identityproviders_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/identityproviders"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: false})
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

type testInstance struct {
	service    *identityproviders.Service
	conn       *pgxpool.Pool
	encryption *encryption.Client
	workos     *mockWorkOSClient
	catalog    *catalogDouble
	serverURL  *url.URL
	orgID      string
}

type mockWorkOSClient struct {
	mock.Mock
}

const testWorkOSRedirectURI = "https://auth.example.test/sso/oidc/conn-example/callback"

type catalogDouble struct {
	mu              sync.Mutex
	searches        map[string][]identityproviders.ApplicationCatalogCandidate
	details         map[string]identityproviders.ApplicationCatalogDetails
	searchErrors    map[string]error
	inspectErrors   map[string]error
	searchCalls     int
	inspectCalls    int
	activeSearches  int
	maxSearches     int
	releaseSearches chan struct{}
	releaseAt       int
}

func newCatalogDouble() *catalogDouble {
	return &catalogDouble{
		searches:        make(map[string][]identityproviders.ApplicationCatalogCandidate),
		details:         make(map[string]identityproviders.ApplicationCatalogDetails),
		searchErrors:    make(map[string]error),
		inspectErrors:   make(map[string]error),
		releaseSearches: nil,
		releaseAt:       0,
	}
}

func (c *catalogDouble) Search(_ context.Context, query string) ([]identityproviders.ApplicationCatalogCandidate, error) {
	c.mu.Lock()
	c.searchCalls++
	c.activeSearches++
	c.maxSearches = max(c.maxSearches, c.activeSearches)
	candidates := append([]identityproviders.ApplicationCatalogCandidate(nil), c.searches[query]...)
	err := c.searchErrors[query]
	releaseSearches := c.releaseSearches
	if releaseSearches != nil && c.activeSearches == c.releaseAt {
		close(releaseSearches)
		c.releaseSearches = nil
	}
	c.mu.Unlock()
	if releaseSearches != nil {
		<-releaseSearches
	}
	c.mu.Lock()
	c.activeSearches--
	c.mu.Unlock()
	return candidates, err
}

func (c *catalogDouble) Inspect(_ context.Context, providerKey, catalogRef string) (identityproviders.ApplicationCatalogDetails, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inspectCalls++
	key := providerKey + "\x00" + catalogRef
	return c.details[key], c.inspectErrors[key]
}

func (c *catalogDouble) SetMatch(label, providerKey, catalogRef, name, remoteURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.searches[label] = []identityproviders.ApplicationCatalogCandidate{{ProviderKey: providerKey, CatalogRef: catalogRef, Name: name}}
	c.details[providerKey+"\x00"+catalogRef] = identityproviders.ApplicationCatalogDetails{ProviderKey: providerKey, CatalogRef: catalogRef, Name: name, RemoteURL: remoteURL}
}

func (c *catalogDouble) SetCandidates(label string, candidates []identityproviders.ApplicationCatalogCandidate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.searches[label] = append([]identityproviders.ApplicationCatalogCandidate(nil), candidates...)
}

func (c *catalogDouble) SetSearchError(label string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.searchErrors[label] = err
}

func (c *catalogDouble) BlockFirstSearchesUntil(count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releaseSearches = make(chan struct{})
	c.releaseAt = count
}

func (c *catalogDouble) Stats() (searchCalls, inspectCalls, maxSearches int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.searchCalls, c.inspectCalls, c.maxSearches
}

func (m *mockWorkOSClient) ListConnections(ctx context.Context, organizationID string) ([]workos.Connection, error) {
	args := m.Called(ctx, organizationID)
	connections, _ := args.Get(0).([]workos.Connection)
	return connections, args.Error(1)
}

func (m *mockWorkOSClient) CreateOIDCConnection(ctx context.Context, input workos.CreateOIDCConnectionInput) (workos.Connection, error) {
	args := m.Called(ctx, input)
	connection, _ := args.Get(0).(workos.Connection)
	return connection, args.Error(1)
}

func (m *mockWorkOSClient) GetConnection(ctx context.Context, connectionID string) (workos.Connection, error) {
	args := m.Called(ctx, connectionID)
	connection, _ := args.Get(0).(workos.Connection)
	return connection, args.Error(1)
}

func expectDirectWorkOSConnection(t *testing.T, ti *testInstance, clientID string, customAuthorizationServer bool) *string {
	t.Helper()

	var clientSecret string
	discoveryEndpoint := "https://example.okta.com/.well-known/openid-configuration"
	if customAuthorizationServer {
		discoveryEndpoint = "https://example.okta.com/oauth2/default/.well-known/openid-configuration"
	}
	ti.workos.On("CreateOIDCConnection", mock.Anything, mock.MatchedBy(func(input workos.CreateOIDCConnectionInput) bool {
		clientSecret = input.ClientSecret
		return input.OrganizationID != "" &&
			input.Name == "Okta" &&
			input.DiscoveryEndpoint == discoveryEndpoint &&
			input.ClientID == clientID &&
			input.ClientSecret != ""
	})).Run(func(_ mock.Arguments) {
		stored, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(t.Context(), ti.orgID)
		require.NoError(t, err)
		require.Equal(t, testSignInAppID, stored.SignInApplicationID.String)
		require.Equal(t, "application_created", stored.SignInState.String)
		if customAuthorizationServer {
			require.True(t, stored.GroupsClaimConfirmed)
			require.Equal(t, "token", stored.GroupsSource.String)
		}
	}).Return(workos.Connection{
		ID:                    "conn-example",
		OrganizationID:        "550e8400-e29b-41d4-a716-446655440000",
		ConnectionType:        "GenericOIDC",
		Name:                  "Okta",
		State:                 "active",
		CallbackEndpoint:      "",
		OIDCDiscoveryEndpoint: "",
		OIDCRedirectURI:       "",
		CreatedAt:             "2026-09-15T00:00:00Z",
		UpdatedAt:             "2026-09-15T00:00:00Z",
	}, nil).Once()
	ti.workos.On("GetConnection", mock.Anything, "conn-example").Return(workos.Connection{
		ID:                    "conn-example",
		OrganizationID:        "550e8400-e29b-41d4-a716-446655440000",
		ConnectionType:        "GenericOIDC",
		Name:                  "Okta",
		State:                 "active",
		CallbackEndpoint:      "https://auth.example.test/sso/oidc/legacy/callback",
		OIDCDiscoveryEndpoint: discoveryEndpoint,
		OIDCRedirectURI:       testWorkOSRedirectURI,
		CreatedAt:             "2026-09-15T00:00:00Z",
		UpdatedAt:             "2026-09-15T00:00:00Z",
	}, nil).Once()
	return &clientSecret
}

func expectWorkOSConnectionRead(t *testing.T, ti *testInstance, organizationID, state string) {
	t.Helper()

	ti.workos.On("GetConnection", mock.Anything, "conn-example").Return(workos.Connection{
		ID:             "conn-example",
		OrganizationID: organizationID,
		ConnectionType: "GenericOIDC",
		Name:           "Okta",
		State:          state,
		CreatedAt:      "2026-09-15T00:00:00Z",
		UpdatedAt:      "2026-09-15T00:00:00Z",
	}, nil).Once()
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()
	return newTestServiceWithOktaEndpoint(t, "")
}

func newTestServiceWithOktaEndpoint(t *testing.T, endpoint string) (context.Context, *testInstance) {
	t.Helper()
	return newTestServiceWithURLs(t, endpoint, "https://api.example.test/")
}

func newTestServiceWithURLs(t *testing.T, oktaEndpoint, publicURL string) (context.Context, *testInstance) {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(
		t,
		logger,
		tracerProvider,
		conn,
		redisClient,
		cache.Suffix("identity-providers"),
		billing.NewStubClient(logger, tracerProvider),
	)
	ctx := authztest.InitAuthContext(t, t.Context(), conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	serverURL, err := url.Parse(publicURL)
	require.NoError(t, err)
	encryptionClient := testenv.NewEncryptionClient(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	retryConfig := guardian.DefaultRetryConfig()
	retryConfig.MaxAttempts = 0
	workOSClient := &mockWorkOSClient{}
	workOSClient.Test(t)
	t.Cleanup(func() { workOSClient.AssertExpectations(t) })
	catalog := newCatalogDouble()
	service := identityproviders.NewService(
		logger,
		tracerProvider,
		conn,
		sessionManager,
		authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		audit.NewLogger(),
		encryptionClient,
		okta.NewClient(logger, guardianPolicy, okta.ClientOpts{Endpoint: oktaEndpoint, RetryConfig: retryConfig}),
		workOSClient,
		catalog,
		serverURL,
	)

	return ctx, &testInstance{
		service:    service,
		conn:       conn,
		encryption: encryptionClient,
		workos:     workOSClient,
		catalog:    catalog,
		serverURL:  serverURL,
		orgID:      authCtx.ActiveOrganizationID,
	}
}

func createConnection(t *testing.T, ctx context.Context, ti *testInstance, tenantURL string) *gen.IdentityProviderConnection {
	t.Helper()

	connection, err := ti.service.Create(ctx, &gen.CreatePayload{
		Kind:         "okta",
		TenantURL:    tenantURL,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	return connection
}

func withExactScope(t *testing.T, ctx context.Context, ti *testInstance, scope authz.Scope) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(scope, ti.orgID))
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func mustUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()

	id, err := uuid.Parse(value)
	require.NoError(t, err)
	return id
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(value)
	require.NoError(t, err)
	return parsed
}
