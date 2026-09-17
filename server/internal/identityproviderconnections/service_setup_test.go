package identityproviderconnections_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// Org URLs the Okta fake factory is seeded for.
const (
	fullOrgURL     = "https://example.okta.com"
	degradedOrgURL = "https://degraded.oktapreview.com"
	rejectedOrgURL = "https://rejected.okta-emea.com"
)

const testClientID = "0oaexampleclient00001"

type serviceInstance struct {
	svc   *identityproviderconnections.Service
	conn  *testInstance
	orgID string
	// flags is nil when the service was built over a caller-supplied provider.
	flags        *feature.InMemory
	oktaFakes    *okta.FakeFactory
	syncTrigger  *fakeSyncTrigger
	discovery    *fakeDiscovery
	provisioner  *identityproviderconnections.Provisioner
	authCtx      *contextvalues.AuthContext
	credentialID uuid.UUID

	// build constructs another service over the same fixtures with a
	// different provisioner (nil models an unconfigured deployment).
	build func(provisioner *identityproviderconnections.Provisioner) *identityproviderconnections.Service
}

func testTime() time.Time {
	return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
}

// fakeDiscovery answers issuer metadata for any org URL, shaped like an Okta
// org authorization server, with overrides for the failure paths.
type fakeDiscovery struct {
	mu            sync.Mutex
	issuerFor     func(orgURL string) string
	tokenEndpoint func(orgURL string) string
	authMethods   []string
	err           error
}

func newFakeDiscovery() *fakeDiscovery {
	return &fakeDiscovery{
		mu:            sync.Mutex{},
		issuerFor:     func(orgURL string) string { return orgURL },
		tokenEndpoint: func(orgURL string) string { return orgURL + "/oauth2/v1/token" },
		authMethods:   []string{"client_secret_basic", "private_key_jwt"},
		err:           nil,
	}
}

func (d *fakeDiscovery) discover(_ context.Context, orgURL string) (remotesessions.DiscoveredIssuerMetadata, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return remotesessions.DiscoveredIssuerMetadata{}, d.err
	}
	return remotesessions.DiscoveredIssuerMetadata{
		Issuer:                                     d.issuerFor(orgURL),
		AuthorizationEndpoint:                      orgURL + "/oauth2/v1/authorize",
		TokenEndpoint:                              d.tokenEndpoint(orgURL),
		RegistrationEndpoint:                       "",
		ScopesSupported:                            []string{"okta.apps.read", "okta.users.read", "okta.groups.read"},
		GrantTypesSupported:                        []string{"client_credentials"},
		ResponseTypesSupported:                     []string{"token"},
		TokenEndpointAuthMethodsSupported:          append([]string(nil), d.authMethods...),
		CodeChallengeMethodsSupported:              []string{"S256"},
		ClientIDMetadataDocumentSupported:          false,
		UserinfoEndpoint:                           "",
		IntrospectionEndpoint:                      "",
		IntrospectionEndpointAuthMethodsSupported:  []string{},
		IDTokenSigningAlgValuesSupported:           []string{},
		ClaimsSupported:                            []string{},
		BackchannelLogoutSupported:                 false,
		AuthorizationResponseIssParameterSupported: false,
		Metadata:          nil,
		UnreadableURL:     "",
		UnreadableMessage: "",
	}, nil
}

// fakeSyncTrigger counts coordinator triggers.
type fakeSyncTrigger struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeSyncTrigger) TriggerApplicationSync(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func (f *fakeSyncTrigger) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// errFlags is a feature provider whose evaluation always fails.
type errFlags struct{}

func (errFlags) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, errors.New("posthog unreachable")
}

func (errFlags) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, errors.New("posthog unreachable")
}

func (errFlags) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, errors.New("posthog unreachable")
}

func allScopes() []string {
	return []string{"okta.apps.read", "okta.users.read", "okta.groups.read"}
}

func oktaFixtures() map[string]okta.Fixtures {
	apps := []okta.App{{ID: "0oaresourceapp000001", Label: "Notion MCP", Name: "oidc_client", SignOnMode: "OPENID_CONNECT", Status: "ACTIVE", Features: nil, Created: testTime(), LastUpdated: testTime()}}
	return map[string]okta.Fixtures{
		"https://unproven.okta.com": {},
		fullOrgURL:                  {Apps: apps, AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: allScopes()},
		degradedOrgURL:              {Apps: apps, AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: []string{"okta.apps.read"}},
		rejectedOrgURL:              {Apps: apps, AppUsers: nil, AppGroups: nil, Groups: nil, GrantedScopes: allScopes()},
	}
}

// newTestService builds the API over a fresh database with the fixture
// provisioner, an in-memory flag switched on for the organization, fake
// discovery, and the Okta fake factory. The returned context carries
// org:admin for the organization.
func newTestService(t *testing.T) (context.Context, *serviceInstance) {
	t.Helper()
	return newTestServiceWithFlags(t, nil)
}

func newTestServiceWithFlags(t *testing.T, features feature.Provider) (context.Context, *serviceInstance) {
	t.Helper()
	return newTestServiceWithPoolLimit(t, features, 0)
}

func newTestServiceWithPoolLimit(t *testing.T, features feature.Provider, maxConns int32) (context.Context, *serviceInstance) {
	t.Helper()

	ctx, ti := newTestDB(t)
	if maxConns > 0 {
		config := ti.conn.Config()
		config.MaxConns = maxConns
		config.MinConns = 0
		pool, err := pgxpool.NewWithConfig(ctx, config)
		require.NoError(t, err)
		t.Cleanup(pool.Close)
		ti.conn = pool
	}
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, ti.conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	ctx = authztest.InitAuthContext(t, ctx, ti.conn, sessionManager)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.ActiveOrganizationID = ti.orgID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	// flags is only the effective provider when the caller supplied none; a
	// service built over another provider exposes nil so toggling it cannot
	// silently do nothing.
	var flags *feature.InMemory
	if features == nil {
		flags = &feature.InMemory{}
		flags.SetFlag(feature.FlagOktaConnections, ti.orgID, true)
		features = flags
	}

	credentialID := provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)
	provisioner := provisiontest.NewProvisioner(t, ti.conn, provisiontest.NewKMSClients(t).Factory, testServerURL, credentialID)
	discovery := newFakeDiscovery()
	fakes := okta.NewFakeFactory(oktaFixtures())
	syncTrigger := &fakeSyncTrigger{}

	authzEngine := authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	build := func(provisioner *identityproviderconnections.Provisioner) *identityproviderconnections.Service {
		return identityproviderconnections.NewService(
			logger,
			tracerProvider,
			testenv.NewMeterProvider(t),
			ti.conn,
			sessionManager,
			authzEngine,
			audit.NewLogger(),
			features,
			provisioner,
			fakes,
			discovery.discover,
			ratelimit.NewRedisStore(redisClient),
			syncTrigger,
		)
	}

	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	return ctx, &serviceInstance{
		svc:          build(provisioner),
		conn:         ti,
		orgID:        ti.orgID,
		flags:        flags,
		oktaFakes:    fakes,
		syncTrigger:  syncTrigger,
		discovery:    discovery,
		provisioner:  provisioner,
		authCtx:      authCtx,
		credentialID: credentialID,
		build:        build,
	}
}

func newUnconfiguredService(t *testing.T, si *serviceInstance) *identityproviderconnections.Service {
	t.Helper()
	return si.build(nil)
}

func mustParseUUID(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(raw)
	require.NoError(t, err)
	return id
}

// asOtherOrganization rebinds the caller onto a second organization with
// org:admin there, for tenant isolation checks.
func asOtherOrganization(t *testing.T, ctx context.Context, si *serviceInstance) (context.Context, string) {
	t.Helper()

	otherOrgID := createOrganization(t, ctx, si.conn.conn)
	other := *si.authCtx
	other.ActiveOrganizationID = otherOrgID
	ctx = contextvalues.SetAuthContext(ctx, &other)
	si.flags.SetFlag(feature.FlagOktaConnections, otherOrgID, true)
	return authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, otherOrgID)), otherOrgID
}

// asSupportSession marks the caller as a platform admin impersonating the org.
func asSupportSession(ctx context.Context, si *serviceInstance) context.Context {
	support := *si.authCtx
	support.IsAdmin = true
	support.SupportOrganizationID = si.orgID
	return contextvalues.WithValidatedSupportSession(ctx, &support)
}

func createConnection(t *testing.T, ctx context.Context, si *serviceInstance, orgURL string) *gen.OktaIdentityProviderConnection {
	t.Helper()

	created, err := si.svc.Create(ctx, &gen.CreatePayload{SessionToken: nil, OrgURL: orgURL, ListingMode: nil})
	require.NoError(t, err)
	require.NotNil(t, created)
	return created
}

func submitClientID(t *testing.T, ctx context.Context, si *serviceInstance, id string) *gen.OktaIdentityProviderConnection {
	t.Helper()

	submitted, err := si.svc.SubmitClientID(ctx, &gen.SubmitClientIDPayload{SessionToken: nil, ID: id, ClientID: testClientID})
	require.NoError(t, err)
	require.NotNil(t, submitted)
	return submitted
}

// managedJWKS returns the document the client JWKS handler serves for a managed client.
func managedJWKS(t *testing.T, ctx context.Context, si *serviceInstance, managed *identityproviderconnections.ManagedClient) string {
	t.Helper()

	row, err := remotesessionsrepo.New(si.conn.conn).GetRemoteSessionClientJsonWebKeySetDocument(ctx, managed.ClientRowID)
	require.NoError(t, err)
	return string(row.Document)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func checklistKeys(items []*gen.IdentityProviderConnectionChecklistItem) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return keys
}
