package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const chainedRefreshResourceIssuer = "https://as.chained-refresh.example.test"

// chainedRefreshFixture is an EMA-only endpoint as identity chaining sees it:
// an organization-level user session issuer trusting an IdP, a ready binding
// naming the endpoint's upstream, the binding's resource client attached with
// no interactive session, and a human holding a refreshable MCP grant.
type chainedRefreshFixture struct {
	ti           *testInstance
	org          string
	human        string
	idpIssuer    uuid.UUID
	idpClient    uuid.UUID
	configHash   string
	slug         string
	clientID     string
	refreshToken string
	resourceKey  string
}

func newChainedRefreshFixture(t *testing.T) (context.Context, chainedRefreshFixture) {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	org, project := authCtx.ActiveOrganizationID, *authCtx.ProjectID
	fixtures := testrepo.New(ti.conn)
	rs := remotesessions_repo.New(ti.conn)
	us := usersessions_repo.New(ti.conn)

	human := "user-" + uuid.NewString()
	require.NoError(t, fixtures.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: human, Email: human + "@example.test", DisplayName: "Chained refresh user"}))
	require.NoError(t, fixtures.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: org, UserID: pgtype.Text{String: human, Valid: true}}))

	idpIssuer, idpClient := uuid.New(), uuid.New()
	require.NoError(t, fixtures.SeedDelegationLoaderIssuerFixture(ctx, testrepo.SeedDelegationLoaderIssuerFixtureParams{ID: idpIssuer, OrganizationID: pgtype.Text{String: org, Valid: true}, Slug: "idp-" + uuid.NewString()[:8], Issuer: "https://idp.chained-refresh.example.test", AuthorizationEndpoint: pgtype.Text{}, TokenEndpoint: pgtype.Text{}, JwksUri: pgtype.Text{}}))
	require.NoError(t, fixtures.SeedDelegationLoaderClientFixture(ctx, testrepo.SeedDelegationLoaderClientFixtureParams{ID: idpClient, OrganizationID: org, RemoteSessionIssuerID: idpIssuer, ClientID: "idp-client", Scope: []string{"openid", "email"}}))
	userIssuer, err := us.CreateOrganizationUserSessionIssuer(ctx, usersessions_repo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID: conv.ToPGText(org), Slug: "chained-refresh-" + uuid.NewString()[:8], AuthnChallengeMode: "chain",
		SessionDuration:              pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: idpIssuer, Valid: true}, TrustedRemoteSessionClientID: uuid.NullUUID{UUID: idpClient, Valid: true},
	})
	require.NoError(t, err)

	// The binding's resource client, registered for the JWT bearer grant.
	resourceIssuer, err := rs.CreateRemoteSessionIssuer(ctx, remotesessions_repo.CreateRemoteSessionIssuerParams{
		OrganizationID: conv.ToPGText(org), Slug: "chained-as-" + uuid.NewString()[:8], Issuer: chainedRefreshResourceIssuer,
		TokenEndpoint:                       conv.ToPGText(chainedRefreshResourceIssuer + "/token"),
		GrantTypesSupported:                 []string{oauthwire.GrantTypeJWTBearer},
		AuthorizationGrantProfilesSupported: []string{oauthwire.GrantProfileIDJAG},
		TokenEndpointAuthMethodsSupported:   []string{oauthwire.AuthMethodClientSecretPost},
		MetadataFetchedAt:                   conv.ToPGTimestamptz(time.Now()),
		ScopesSupported:                     []string{}, ResponseTypesSupported: []string{}, CodeChallengeMethodsSupported: []string{},
		IntrospectionEndpointAuthMethodsSupported: []string{}, IDTokenSigningAlgValuesSupported: []string{}, ClaimsSupported: []string{},
	})
	require.NoError(t, err)
	secret, err := ti.enc.Encrypt([]byte("resource-secret"))
	require.NoError(t, err)
	resourceClient, err := rs.CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		OrganizationID: conv.ToPGText(org), RemoteSessionIssuerID: resourceIssuer.ID, ClientID: "chained-resource-client",
		ClientSecretEncrypted: conv.ToPGText(secret), TokenEndpointAuthMethod: conv.ToPGText(oauthwire.AuthMethodClientSecretPost), Scope: []string{},
	})
	require.NoError(t, err)
	_, err = rs.SetEMAClientGrants(ctx, remotesessions_repo.SetEMAClientGrantsParams{GrantTypes: []string{oauthwire.GrantTypeJWTBearer}, ID: resourceClient.ID, ProjectID: uuid.NullUUID{}, OrganizationID: conv.ToPGText(org)})
	require.NoError(t, err)
	require.NoError(t, rs.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessions_repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: resourceClient.ID, UserSessionIssuerID: userIssuer.ID}))

	const upstream = "https://api.chained-refresh.example.test/mcp"
	key := remotesessions_repo.GetEMABindingParams{ProjectID: project, OrganizationID: org, UserSessionIssuerID: userIssuer.ID, RemoteSessionIssuerID: resourceIssuer.ID, Resource: upstream}
	require.NoError(t, rs.EnsureEMABinding(ctx, remotesessions_repo.EnsureEMABindingParams(key)))
	binding, err := rs.GetEMABinding(ctx, key)
	require.NoError(t, err)
	_, err = rs.SetEMABinding(ctx, remotesessions_repo.SetEMABindingParams{
		State: conv.ToPGText(remotesessions.PreparationStateReady), RemoteSessionClientID: uuid.NullUUID{UUID: resourceClient.ID, Valid: true},
		Generation: binding.Generation + 1, GrantSource: conv.ToPGText(remotesessions.PreparationGrantSourceAdministratorDeclared),
		RequestedScopes: []string{"read"}, ClaimID: uuid.NullUUID{}, ClaimedAt: pgtype.Timestamptz{},
		ID: binding.ID, ProjectID: project, OrganizationID: org, ExpectedGeneration: binding.Generation,
	})
	require.NoError(t, err)

	slug := "chained-refresh-" + uuid.NewString()[:8]
	serverID := attachConsentRemoteMcpServer(t, ctx, ti.conn, project, userIssuer.ID, slug, upstream)
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: project, CustomDomainID: uuid.NullUUID{}, McpServerID: conv.ToNullUUID(serverID), MetaMcpServerID: uuid.NullUUID{}, Slug: slug,
	})
	require.NoError(t, err)
	resourceKey := cacheEMAEndpointResource(t, ctx, ti, org, project, upstream, protectedresource.EndpointResource{Resource: upstream, AuthorizationServers: []string{chainedRefreshResourceIssuer}, Discovered: true})

	client, err := us.CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID: userIssuer.ID, ClientID: "chained-refresh-client-" + uuid.NewString()[:8], ClientName: "chained refresh client",
		RedirectUris: []string{"http://localhost:3000/callback"}, TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)
	refreshToken := "refresh-" + uuid.NewString()
	refreshHash := sha256.Sum256([]byte(refreshToken))
	_, err = us.CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: userIssuer.ID, UserSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true},
		SubjectUrn: urn.NewUserSubject(human), Jti: uuid.NewString(),
		RefreshTokenHash: conv.ToPGText(base64.RawURLEncoding.EncodeToString(refreshHash[:])),
		RefreshExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Minute)),
		SignInID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	trusted, err := rs.GetTrustedRemoteSessionClientForOrganization(ctx, remotesessions_repo.GetTrustedRemoteSessionClientForOrganizationParams{OrganizationID: org, IssuerID: idpIssuer, ClientID: idpClient})
	require.NoError(t, err)
	return ctx, chainedRefreshFixture{
		ti: ti, org: org, human: human, idpIssuer: idpIssuer, idpClient: idpClient,
		configHash: remotesessions.FederatedDelegationConfigurationHash(org, trusted.RemoteSessionIssuer, trusted.RemoteSessionClient),
		slug:       slug, clientID: client.ClientID, refreshToken: refreshToken, resourceKey: resourceKey,
	}
}

// cacheEMAEndpointResource stores discovery's answer for endpoint as
// ChallengeManager.ResolveEMAResource caches it, and returns the cache key.
func cacheEMAEndpointResource(t *testing.T, ctx context.Context, ti *testInstance, org string, project uuid.UUID, endpoint string, resource protectedresource.EndpointResource) string {
	t.Helper()
	digest := sha256.Sum256([]byte(org + "\n" + project.String() + "\n" + endpoint))
	key := "emaEndpointResource:v1:" + hex.EncodeToString(digest[:])
	entry := struct {
		Resource protectedresource.EndpointResource `json:"resource"`
		Invalid  bool                               `json:"invalid"`
		Failed   bool                               `json:"failed"`
	}{Resource: resource, Invalid: false, Failed: false}
	require.NoError(t, ti.cacheAdapter.Set(ctx, key, entry, time.Hour))
	return key
}

// installNonProbingChainer serves the fixture with a production chainer whose
// live discovery fails the test, reading cached discovery through cached.
func (f chainedRefreshFixture) installNonProbingChainer(t *testing.T, cached func(*remotesessions.ChallengeManager) func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, bool, error)) {
	t.Helper()
	logger := testenv.NewLogger(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	challenges := remotesessions.NewChallengeManager(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), f.ti.conn, f.ti.enc, policy, nil, f.ti.cacheAdapter, f.ti.serverURL)
	chainer := identitychaining.New(logger, f.ti.conn, f.ti.enc, challenges, remotesessions.NewDelegationService(f.ti.conn, f.ti.enc, challenges), nil, f.ti.cacheAdapter)
	chainer.SetResourceResolver(func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, error) {
		t.Error("the token endpoint must never probe the upstream")
		return protectedresource.EndpointResource{}, errors.New("unexpected probe")
	})
	chainer.SetCachedResourceResolver(cached(challenges))
	f.ti.service.SetIdentityChainer(chainer)
}

// retainDelegation records the human's retained delegation in status, its
// assertion already expired; a durable one also holds a refresh grant.
func (f chainedRefreshFixture) retainDelegation(t *testing.T, ctx context.Context, status string) {
	t.Helper()
	refresh := pgtype.Text{}
	if status == "durable_credential_present" {
		refresh = conv.ToPGText("encrypted-refresh")
	}
	_, err := remotesessions_repo.New(f.ti.conn).UpsertTrustedDelegationCredential(ctx, remotesessions_repo.UpsertTrustedDelegationCredentialParams{
		OrganizationID: f.org, ClientID: f.idpClient, IssuerID: f.idpIssuer, SubjectUrn: urn.NewUserSubject(f.human).String(), ExpectedGeneration: 0,
		CredentialConfigHash:       conv.ToPGText(f.configHash),
		ObservationStatus:          conv.ToPGText(status),
		IdentityAssertionEncrypted: conv.ToPGText("encrypted-assertion"),
		IdentityAssertionExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Minute)),
		RefreshTokenEncrypted:      refresh,
	})
	require.NoError(t, err)
}

func (f chainedRefreshFixture) refresh(t *testing.T, ctx context.Context) refreshResult {
	t.Helper()
	res := performRefreshRequest(ctx, f.ti, f.slug, f.clientID, f.refreshToken)
	require.NoError(t, res.err)
	return res
}

func requireRefreshReconnect(t *testing.T, res refreshResult) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, res.code, res.body)
	require.Contains(t, res.body, `"error":"invalid_grant"`)
	require.Contains(t, res.body, reconnectDescription)
}

// The default test service runs the production chainer and resource
// resolvers, so these refreshes are judged by a real Governor over real
// bindings, delegations and the shared discovery cache.
func TestHandleToken_ChainedRefreshServedByRetainedDelegation(t *testing.T) {
	t.Parallel()
	ctx, f := newChainedRefreshFixture(t)
	f.retainDelegation(t, ctx, "durable_credential_present")
	res := f.refresh(t, ctx)
	require.Equal(t, http.StatusOK, res.code, res.body)
}

func TestHandleToken_ChainedRefreshNeedsReconnectWhenDelegationNeedsReauthentication(t *testing.T) {
	t.Parallel()
	ctx, f := newChainedRefreshFixture(t)
	f.retainDelegation(t, ctx, "reauthentication_required")
	requireRefreshReconnect(t, f.refresh(t, ctx))
}

func TestHandleToken_ChainedRefreshNeedsReconnectWhenNoProviderCanServe(t *testing.T) {
	t.Parallel()
	ctx, f := newChainedRefreshFixture(t)
	f.retainDelegation(t, ctx, "durable_credential_present")
	fixtures := testrepo.New(f.ti.conn)
	keys, err := fixtures.SeedJsonWebKeySetFixture(ctx, testrepo.SeedJsonWebKeySetFixtureParams{OrganizationID: f.org, Name: "speakeasy-issuer-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	internal, err := fixtures.SeedInternalSessionIssuerFixture(ctx, testrepo.SeedInternalSessionIssuerFixtureParams{OrganizationID: f.org, Issuer: "https://speakeasy.example.test/oauth/idp/org/" + f.org, JsonWebKeySetID: keys})
	require.NoError(t, err)
	vendor, err := remotesessions.NormalizeVendorIssuer(chainedRefreshResourceIssuer)
	require.NoError(t, err)
	// Only the Speakeasy provider, which the test deployment cannot sign with,
	// so the runtime answers every call with a reconnect challenge.
	require.NoError(t, remotesessions_repo.New(f.ti.conn).InsertEMAProvider(ctx, remotesessions_repo.InsertEMAProviderParams{
		OrganizationID: f.org, VendorIssuer: vendor,
		RemoteSessionIssuerID:   uuid.NullUUID{},
		InternalSessionIssuerID: uuid.NullUUID{UUID: internal, Valid: true},
		Position:                0,
	}))
	requireRefreshReconnect(t, f.refresh(t, ctx))
}

func TestHandleToken_ChainedRefreshNeedsReconnectAfterTrustedRegistrationRotated(t *testing.T) {
	t.Parallel()
	ctx, f := newChainedRefreshFixture(t)
	f.retainDelegation(t, ctx, "durable_credential_present")
	rotated, err := f.ti.enc.Encrypt([]byte("rotated-secret"))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(f.ti.conn).UpdateOrganizationRemoteSessionClient(ctx, remotesessions_repo.UpdateOrganizationRemoteSessionClientParams{
		ClientSecretEncrypted: conv.ToPGText(rotated), ID: f.idpClient, OrganizationID: conv.ToPGText(f.org),
	})
	require.NoError(t, err)
	requireRefreshReconnect(t, f.refresh(t, ctx))
}

func TestHandleToken_ChainedRefreshProceedsThroughTransientDiscoveryFailure(t *testing.T) {
	t.Parallel()
	ctx, f := newChainedRefreshFixture(t)
	// A delegation that would otherwise demand a reconnect.
	f.retainDelegation(t, ctx, "reauthentication_required")
	f.installNonProbingChainer(t, func(*remotesessions.ChallengeManager) func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, bool, error) {
		return func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, bool, error) {
			return protectedresource.EndpointResource{}, true, errors.New("endpoint resource discovery temporarily unavailable")
		}
	})
	res := f.refresh(t, ctx)
	require.Equal(t, http.StatusOK, res.code, "a transient fault never costs the client its session: %s", res.body)
}

func TestHandleToken_ChainedRefreshWithColdDiscoveryCacheProceeds(t *testing.T) {
	t.Parallel()
	ctx, f := newChainedRefreshFixture(t)
	f.retainDelegation(t, ctx, "reauthentication_required")
	f.installNonProbingChainer(t, func(challenges *remotesessions.ChallengeManager) func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, bool, error) {
		return challenges.CachedEMAResource
	})
	requireRefreshReconnect(t, f.refresh(t, ctx))

	require.NoError(t, f.ti.cacheAdapter.Delete(ctx, f.resourceKey))
	res := f.refresh(t, ctx)
	require.Equal(t, http.StatusOK, res.code, "without cached discovery the refresh cannot name the runtime's binding, so it proceeds: %s", res.body)
}
