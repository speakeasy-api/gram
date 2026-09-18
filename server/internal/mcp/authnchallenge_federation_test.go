package mcp_test

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

type federatedLoginConsumerFunc func(context.Context, mcp.AuthorizedFederatedLogin) error

func (f federatedLoginConsumerFunc) ConsumeFederatedLogin(ctx context.Context, login mcp.AuthorizedFederatedLogin) error {
	return f(ctx, login)
}

func TestFederatedLoginWrongPKCEVerifier(t *testing.T) {
	t.Parallel()
	runFederatedLogin(t, "wrong_verifier", nil)
}

func TestFederatedLoginUserSnapshotDetectsSameValueUpdate(t *testing.T) {
	t.Parallel()
	// Use the browser suite's cloned database and pgxpool autocommit, not a
	// wrapping transaction: xmin must change even when the email does not.
	ctx, f := newFederationLoginFixture(t, true)
	queries := usersrepo.New(f.ti.conn)
	user, err := queries.GetUser(ctx, mockidp.MockUserID)
	require.NoError(t, err)
	params := usersrepo.SnapshotOrganizationUsersFixtureParams{UserID: mockidp.MockUserID, OrganizationIds: []string{f.organizationID, f.otherOrg}}
	before, err := queries.SnapshotOrganizationUsersFixture(ctx, params)
	require.NoError(t, err)
	require.NotEmpty(t, before)
	unchanged, err := queries.SnapshotOrganizationUsersFixture(ctx, params)
	require.NoError(t, err)
	require.Equal(t, before, unchanged, "reads must not change the snapshot")
	err = queries.SetOrganizationUserEmailFixture(ctx, usersrepo.SetOrganizationUserEmailFixtureParams{Email: user.Email, UserID: mockidp.MockUserID, OrganizationID: f.organizationID})
	require.NoError(t, err)
	after, err := queries.SnapshotOrganizationUsersFixture(ctx, params)
	require.NoError(t, err)
	require.NotEqual(t, before, after, "same-value autocommit updates must change xmin")
	unchangedUser, err := queries.GetUser(ctx, mockidp.MockUserID)
	require.NoError(t, err)
	require.Equal(t, user, unchangedUser, "the update must preserve visible user values")
}

func TestFederatedLoginBrowserBinding(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"missing_cookie", "wrong_cookie", "expired_state"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			runFederatedLogin(t, scenario, nil)
		})
	}
}

func TestFederatedLoginProviderBinding(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"changed_client", "deleted_client", "changed_issuer", "missing_response_issuer", "wrong_response_issuer", "other_provider_token"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			runFederatedLogin(t, scenario, nil)
		})
	}
}

func TestFederatedLoginIdentityClaims(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"wrong_nonce", "missing_email", "unverified_email"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			runFederatedLogin(t, scenario, nil)
		})
	}
}

func TestFederatedLoginProvisionedHuman(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"unprovisioned", "deleted_directory_user", "wrong_tenant_directory", "deleted_gram_user", "nonexistent_gram_account", "foreign_membership", "ambiguous_gram_accounts", "workos_deleted_directory"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			runFederatedLogin(t, scenario, nil)
		})
	}
}

func TestFederatedLoginCredentialHandoff(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"consumer", "consumer_failure", "membership_denied"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			runFederatedLogin(t, scenario, nil)
		})
	}
}

func TestFederatedLoginConsent(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"success", "first_party"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			runFederatedLogin(t, scenario, nil)
		})
	}
}

// Exercise the browser protocol with real PostgreSQL/Redis and signed TLS OIDC
// responses. Each invocation owns its provider, tenant, browser state and consumer.
type federationRotation func(context.Context, *testInstance, *federationProvider, uuid.UUID, string, *http.Request, *http.Request) (*http.Request, string, string)

func runFederatedLogin(t *testing.T, scenario string, rotate federationRotation) {
	t.Helper()
	ctx, f := newFederationLoginFixture(t, scenario != "membership_denied")
	ti, provider, resolver := f.ti, f.provider, f.resolver
	expectedSecret := "selected-secret"
	var err error
	var consumed int
	var retainedIdentity *remotesessions.FederatedIdentity
	if scenario != "success" && scenario != "first_party" {
		ti.service.SetFederatedLoginConsumer(federatedLoginConsumerFunc(func(_ context.Context, login mcp.AuthorizedFederatedLogin) error {
			consumed++
			require.Equal(t, []string{mockidp.MockUserID}, resolver.memberChecks, "membership must precede credential handoff")
			require.Equal(t, f.organizationID, login.OrganizationID)
			require.Equal(t, mockidp.MockUserID, login.UserID)
			require.Equal(t, f.issuerID, login.UserSessionIssuerID)
			require.Equal(t, f.remoteIssuerID, login.TrustedIssuerID)
			require.Equal(t, f.clientID, login.TrustedClientID)
			retainedIdentity = login.Identity
			require.NoError(t, login.Identity.WithCredentials(func(credentials remotesessions.EphemeralFederatedCredentials) error {
				require.NotEmpty(t, credentials.IDToken())
				require.Empty(t, credentials.RefreshToken())
				return nil
			}))
			require.Error(t, login.Identity.WithCredentials(func(remotesessions.EphemeralFederatedCredentials) error {
				t.Error("credentials consumed twice")
				return nil
			}))
			if scenario == "consumer_failure" {
				return errors.New("sensitive upstream failure")
			}
			return nil
		}))
	}
	// Mutate only this tenant's fixture before recording the no-upsert
	// snapshot. The directory remains NULL-linked in every fallback case.
	switch scenario {
	case "deleted_gram_user":
		err = usersrepo.New(ti.conn).SoftDeleteOrganizationUserFixture(ctx, usersrepo.SoftDeleteOrganizationUserFixtureParams{UserID: mockidp.MockUserID, OrganizationID: f.organizationID})
	case "nonexistent_gram_account":
		err = usersrepo.New(ti.conn).SetOrganizationUserEmailFixture(ctx, usersrepo.SetOrganizationUserEmailFixtureParams{Email: "different-account@example.test", UserID: mockidp.MockUserID, OrganizationID: f.organizationID})
	case "foreign_membership":
		_, err = orgsrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgsrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: f.otherOrg, UserID: conv.ToPGText(mockidp.MockUserID)})
		require.NoError(t, err)
		err = testrepo.New(ti.conn).DeleteOrganizationUserRelationshipFixture(ctx, testrepo.DeleteOrganizationUserRelationshipFixtureParams{OrganizationID: f.organizationID, UserID: conv.ToPGText(mockidp.MockUserID)})
	case "ambiguous_gram_accounts":
		// The raw email is unique, but resolution deliberately matches case
		// insensitively. Both active humans have same-organization membership.
		duplicateID := "ambiguous-" + uuid.NewString()
		err = usersrepo.New(ti.conn).DuplicateOrganizationUserEmailFixture(ctx, usersrepo.DuplicateOrganizationUserEmailFixtureParams{DuplicateUserID: duplicateID, UserID: mockidp.MockUserID, OrganizationID: f.organizationID})
		require.NoError(t, err)
		_, err = orgsrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgsrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: f.organizationID, UserID: conv.ToPGText(duplicateID)})
	case "workos_deleted_directory":
		err = directoryrepo.New(ti.conn).SetOrganizationDirectoryUserDeletionFixture(ctx, directoryrepo.SetOrganizationDirectoryUserDeletionFixtureParams{OrganizationID: f.organizationID, LocalDeleted: false, WorkosDeleted: true})
	}
	require.NoError(t, err)
	beforeUsers, err := usersrepo.New(ti.conn).SnapshotOrganizationUsersFixture(ctx, usersrepo.SnapshotOrganizationUsersFixtureParams{UserID: mockidp.MockUserID, OrganizationIds: []string{f.organizationID, f.otherOrg}})
	require.NoError(t, err)
	req, id, nonce, challenge, state, cookie := f.begin(t, ctx, scenario == "first_party")
	switch scenario {
	case "wrong_verifier":
		// Keep RFC 7636 syntax valid so rejection happens at the provider.
		const wrongVerifier = "wrong-but-nonempty-verifier-with-at-least-43-characters"
		require.NotEqual(t, wrongVerifier, state.Federation.Verifier)
		state.Federation.Verifier = wrongVerifier
		require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	case "expired_state":
		state.CreatedAt = time.Now().Add(-state.TTL() - time.Minute)
		require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	case "changed_client":
		err = remotesessionsrepo.New(ti.conn).SetOrganizationRemoteSessionClientCredentialsFixture(ctx, remotesessionsrepo.SetOrganizationRemoteSessionClientCredentialsFixtureParams{ID: f.clientID, OrganizationID: conv.ToPGText(f.organizationID), ClientID: conv.ToPGText("changed"), ClientSecretEncrypted: pgtype.Text{String: "", Valid: false}})
	case "deleted_client":
		err = remotesessionsrepo.New(ti.conn).SoftDeleteOrganizationRemoteSessionClientFixture(ctx, remotesessionsrepo.SoftDeleteOrganizationRemoteSessionClientFixtureParams{ID: f.clientID, OrganizationID: conv.ToPGText(f.organizationID)})
	case "changed_issuer":
		err = usersessionsrepo.New(ti.conn).AdvanceOrganizationUserSessionIssuerVersionFixture(ctx, usersessionsrepo.AdvanceOrganizationUserSessionIssuerVersionFixtureParams{ID: f.issuerID, OrganizationID: conv.ToPGText(f.organizationID)})
	case "wrong_tenant_directory":
		err = directoryrepo.New(ti.conn).DeleteOrganizationDirectoryUsersFixture(ctx, f.organizationID)
	case "deleted_directory_user":
		err = directoryrepo.New(ti.conn).SetOrganizationDirectoryUserDeletionFixture(ctx, directoryrepo.SetOrganizationDirectoryUserDeletionFixtureParams{OrganizationID: f.organizationID, LocalDeleted: true, WorkosDeleted: false})
	}
	require.NoError(t, err)
	email, tokenNonce, tokenIssuer := mockidp.MockUserEmail, nonce, provider.URL
	if scenario == "missing_email" {
		email = ""
	}
	if scenario == "unprovisioned" {
		email = "unprovisioned@example.test"
	}
	if scenario == "wrong_nonce" {
		tokenNonce = "wrong-nonce"
	}
	if scenario == "other_provider_token" {
		tokenIssuer = f.otherProviderURL
	}
	provider.issueCode(t, "one-use-code", federationToken{challenge: challenge, expectPKCERejection: scenario == "wrong_verifier", nonce: tokenNonce, email: email, issuer: tokenIssuer, verified: scenario != "unverified_email", secret: expectedSecret})
	callbackQuery := url.Values{"state": {id}, "code": {"one-use-code"}, "iss": {provider.URL}}
	if scenario == "missing_response_issuer" {
		callbackQuery.Del("iss")
	}
	if scenario == "wrong_response_issuer" {
		callbackQuery.Set("iss", "https://other.example.test")
	}
	callback := httptest.NewRequest(http.MethodGet, ti.serverURL.String()+"/mcp/idp_callback?"+callbackQuery.Encode(), nil).WithContext(ctx)
	if scenario == "wrong_cookie" {
		cookie.Value = "different-browser"
	}
	if scenario != "missing_cookie" {
		callback.AddCookie(cookie)
	}
	if rotate != nil {
		callback, nonce, id = rotate(ctx, ti, provider, f.clientID, f.organizationID, req, callback)
		expectedSecret = "rotated-secret"
	}
	result := httptest.NewRecorder()
	err = ti.service.HandleIDPCallback(result, callback)
	success := scenario == "success" || scenario == "first_party" || scenario == "consumer"
	if success {
		require.NoError(t, err)
		require.Equal(t, http.StatusFound, result.Code)
		consent, err := url.Parse(result.Header().Get("Location"))
		require.NoError(t, err)
		require.Contains(t, consent.Path, "/connect")
		consentID := consent.Query().Get("state")
		require.NotEmpty(t, consentID)
		require.NotEqual(t, id, consentID)
		resolved, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+consentID)
		require.NoError(t, err)
		require.Nil(t, resolved.Federation)
		require.NotNil(t, resolved.Browser)
		if rotate == nil {
			require.Equal(t, state.Browser, resolved.Browser)
		} else {
			require.NotEqual(t, state.Browser.CookieID, resolved.Browser.CookieID)
		}
		require.NotNil(t, resolved.Subject)
		require.Equal(t, mockidp.MockUserID, resolved.AuthorizerUserID)
		require.Equal(t, scenario == "first_party", resolved.FirstParty)
		require.Equal(t, []string{mockidp.MockUserID}, resolver.memberChecks)
		require.Equal(t, 1, provider.exchangeCount())
		serialized, err := json.Marshal(resolved)
		require.NoError(t, err)
		require.NotContains(t, string(serialized), "ephemeral-access-token")
		require.NotContains(t, string(serialized), nonce)
	} else {
		require.NoError(t, err, "handled OAuth failures redirect to the registered client")
		failure := assertFederationErrorRedirect(t, result)
		for _, sensitive := range []string{mockidp.MockUserEmail, "ephemeral-access-token", "sensitive upstream failure", expectedSecret} {
			require.NotContains(t, failure.String(), sensitive)
		}
	}
	switch scenario {
	case "deleted_gram_user", "nonexistent_gram_account", "foreign_membership", "ambiguous_gram_accounts", "workos_deleted_directory":
		failure, parseErr := url.Parse(result.Header().Get("Location"))
		require.NoError(t, parseErr)
		require.Equal(t, "Your account is not provisioned for this organization. Contact your administrator", failure.Query().Get("error_description"))
		require.Empty(t, resolver.memberChecks, "provisioned-human resolution must reject before the membership hook")
		require.Zero(t, consumed, "unresolved humans must never reach credential handoff")
	}
	require.Zero(t, f.otherProviderCalls.Load(), "never contact a provider from another organization")
	if scenario == "consumer" || scenario == "consumer_failure" {
		require.Equal(t, 1, consumed)
		require.Error(t, retainedIdentity.WithCredentials(func(remotesessions.EphemeralFederatedCredentials) error {
			t.Error("handoff replayed after callback")
			return nil
		}))
	} else {
		require.Zero(t, consumed)
	}
	if scenario == "membership_denied" {
		require.Equal(t, []string{mockidp.MockUserID}, resolver.memberChecks)
	}
	require.NotContains(t, resolver.calls, "CompleteIDPLogin", "federation must never upsert a user")
	afterUsers, err := usersrepo.New(ti.conn).SnapshotOrganizationUsersFixture(ctx, usersrepo.SnapshotOrganizationUsersFixtureParams{UserID: mockidp.MockUserID, OrganizationIds: []string{f.organizationID, f.otherOrg}})
	require.NoError(t, err)
	require.Equal(t, beforeUsers, afterUsers)
	count := provider.exchangeCount()
	require.Error(t, ti.service.HandleIDPCallback(httptest.NewRecorder(), callback), "callback is single-use even on rejection")
	require.Equal(t, count, provider.exchangeCount(), "replay must not reach the token endpoint")
	if scenario == "missing_cookie" || scenario == "wrong_cookie" || scenario == "expired_state" || scenario == "changed_client" || scenario == "deleted_client" || scenario == "changed_issuer" || scenario == "missing_response_issuer" || scenario == "wrong_response_issuer" {
		require.Zero(t, provider.exchangeCount())
	} else {
		require.Equal(t, 1, provider.exchangeCount(), "identity and membership failures must exercise verified exchange")
	}
	require.LessOrEqual(t, consumed, 1, "callback replay must not repeat consumer")
}

// Only fixture construction touches the cross-tenant baseline. Scenario changes
// happen afterward, before recording the no-upsert snapshot.
type federationLoginFixture struct {
	ti                                 *testInstance
	provider                           *federationProvider
	otherProviderCalls                 *atomic.Int32
	otherProviderURL                   string
	resolver                           *mockIdentityResolver
	organizationID, otherOrg           string
	clientID, issuerID, remoteIssuerID uuid.UUID
	toolsetSlug, downstreamClientID    string
}

func newFederationLoginFixture(t *testing.T, memberAllowed bool, offline ...bool) (context.Context, *federationLoginFixture) {
	t.Helper()
	provider := newFederationProvider(t)
	var err error
	var otherProviderCalls atomic.Int32
	otherProvider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherProviderCalls.Add(1)
		http.Error(w, "wrong tenant provider selected", http.StatusForbidden)
	}))
	t.Cleanup(otherProvider.Close)
	roots := x509.NewCertPool()
	roots.AddCert(provider.Certificate())
	roots.AddCert(otherProvider.Certificate())
	resolver := &mockIdentityResolver{hasAccessOK: memberAllowed}
	ctx, ti := newTestMCPServiceWithTunnelPublicConfig(t, resolver, mcp.TunnelPublicConfig{}, guardian.WithTLSRootCAs(roots))
	// Federation requires an HTTPS callback, unlike the shared HTTP fixture.
	ti.serverURL.Scheme = "https"
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherOrg := "org_" + uuid.NewString()
	require.NoError(t, orgsrepo.New(ti.conn).CreateOrganizationMetadata(ctx, orgsrepo.CreateOrganizationMetadataParams{ID: otherOrg, Name: "Other federation organization", Slug: "other-fed-" + uuid.NewString()}))
	// Same external client id and user email in another tenant must not affect
	// provider selection or provisioned-human resolution in this organization.
	otherIssuer := createTrustedIDJAGIssuer(t, ctx, ti, otherOrg, otherProvider.URL, otherProvider.URL+"/jwks")
	_, err = remotesessionsrepo.New(ti.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{OrganizationID: conv.ToPGText(otherOrg), RemoteSessionIssuerID: otherIssuer.TrustedRemoteSessionIssuerID.UUID, ClientID: "selected-client", TokenEndpointAuthMethod: conv.ToPGText("none"), Scope: []string{"openid", "email"}})
	require.NoError(t, err)
	seedIDJAGDirectoryUser(t, ctx, ti, otherOrg)
	remote, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), Slug: "federation-" + uuid.NewString(), Issuer: provider.URL,
		AuthorizationEndpoint: conv.ToPGText(provider.URL + "/authorize"), TokenEndpoint: conv.ToPGText(provider.URL + "/token"), JwksUri: conv.ToPGText(provider.URL + "/jwks"),
		ScopesSupported: []string{"openid", "email"}, GrantTypesSupported: []string{"authorization_code"}, ResponseTypesSupported: []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"}, CodeChallengeMethodsSupported: []string{"S256"}, IDTokenSigningAlgValuesSupported: []string{"ES256"}, AuthorizationResponseIssParameterSupported: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	secret, err := ti.enc.Encrypt([]byte("selected-secret"))
	require.NoError(t, err)
	// Insert an unselected client first: selection must follow the explicit FK.
	_, err = remotesessionsrepo.New(ti.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), RemoteSessionIssuerID: remote.ID, ClientID: "not-selected", ClientSecretEncrypted: conv.ToPGText(secret), TokenEndpointAuthMethod: conv.ToPGText("client_secret_basic"), Scope: []string{"openid"}})
	require.NoError(t, err)
	scopes := []string{"openid", "email"}
	if len(offline) > 0 && offline[0] {
		scopes = append(scopes, "offline_access", "profile")
	}
	client, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), RemoteSessionIssuerID: remote.ID, ClientID: "selected-client", ClientSecretEncrypted: conv.ToPGText(secret), TokenEndpointAuthMethod: conv.ToPGText("client_secret_basic"), Scope: scopes})
	require.NoError(t, err)
	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), Slug: "federation-" + uuid.NewString(), AuthnChallengeMode: "chain", SessionDuration: pgtype.Interval{Microseconds: int64(8 * time.Hour / time.Microsecond), Valid: true}, TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, TrustedRemoteSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true}})
	require.NoError(t, err)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	toolset, err = toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true}, Slug: toolset.Slug, ProjectID: *ac.ProjectID})
	require.NoError(t, err)
	downstream := createIDJAGClient(t, ctx, ti, issuer.ID)
	seedIDJAGDirectoryUser(t, ctx, ti, ac.ActiveOrganizationID)
	// AIM-75: NULL directory link resolves only an existing same-org human.
	err = directoryrepo.New(ti.conn).ClearOrganizationDirectoryUserLinksFixture(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	return ctx, &federationLoginFixture{ti: ti, provider: provider, otherProviderCalls: &otherProviderCalls, otherProviderURL: otherProvider.URL, resolver: resolver, organizationID: ac.ActiveOrganizationID, otherOrg: otherOrg, clientID: client.ID, issuerID: issuer.ID, remoteIssuerID: remote.ID, toolsetSlug: toolset.McpSlug.String, downstreamClientID: downstream.ClientID}
}

// begin checks the bootstrap-to-browser transition, PKCE, and cookie security.
func (f *federationLoginFixture) begin(t *testing.T, ctx context.Context, firstParty bool) (*http.Request, string, string, string, mcp.AuthnChallengeState, *http.Cookie) {
	t.Helper()
	ti, provider := f.ti, f.provider
	var err error
	query := url.Values{"response_type": {"code"}, "client_id": {f.downstreamClientID}, "redirect_uri": {"http://127.0.0.1/callback"}, "state": {"downstream-state"}, "code_challenge": {"downstream-pkce"}, "code_challenge_method": {"S256"}}
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", f.toolsetSlug)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+f.toolsetSlug+"/authorize?"+query.Encode(), nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
	start := httptest.NewRecorder()
	if firstParty {
		err = ti.service.HandleFirstPartyConnect(start, req)
	} else {
		err = ti.service.HandleAuthorize(start, req)
	}
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, start.Code)
	bootstrap, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, ti.serverURL.Host, bootstrap.Host)
	require.Equal(t, "/mcp/idp_callback", bootstrap.Path)
	require.Equal(t, "1", bootstrap.Query().Get("federated_start"))
	initialID := bootstrap.Query().Get("state")
	require.NotEmpty(t, initialID)
	initial, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+initialID)
	require.NoError(t, err)
	require.NotNil(t, initial.Federation)
	require.NotEmpty(t, initial.Federation.BrowserHash)
	begin := httptest.NewRecorder()
	bootstrapRequest := httptest.NewRequest(http.MethodGet, bootstrap.String(), nil).WithContext(ctx)
	cookies := start.Result().Cookies()
	require.Len(t, cookies, 1)
	bootstrapRequest.AddCookie(cookies[0])
	require.NoError(t, ti.service.HandleIDPCallback(begin, bootstrapRequest))
	upstream, err := url.Parse(begin.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, provider.URL+"/authorize", upstream.Scheme+"://"+upstream.Host+upstream.Path)
	require.Equal(t, "selected-client", upstream.Query().Get("client_id"))
	require.NotContains(t, upstream.Query().Get("scope"), "offline_access", "unknown human always starts with minimal login")
	require.Empty(t, upstream.Query().Get("prompt"))
	require.Equal(t, ti.serverURL.String()+"/mcp/idp_callback", upstream.Query().Get("redirect_uri"))
	id := upstream.Query().Get("state")
	require.NotEmpty(t, id)
	require.NotEqual(t, initialID, id)
	nonce := upstream.Query().Get("nonce")
	require.NotEmpty(t, nonce)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+id)
	require.NoError(t, err)
	digest := sha256.Sum256([]byte(state.Federation.Verifier))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(digest[:]), upstream.Query().Get("code_challenge"))
	require.Equal(t, "S256", upstream.Query().Get("code_challenge_method"))
	require.Empty(t, begin.Result().Cookies())
	cookie := cookies[0]
	require.Empty(t, cookie.Domain)
	require.Equal(t, "/", cookie.Path)
	require.True(t, cookie.Secure)
	require.True(t, cookie.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	require.Contains(t, cookie.Name, "__Host-")
	require.Error(t, ti.service.HandleIDPCallback(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, bootstrap.String(), nil).WithContext(ctx)), "bootstrap is single-use")
	return req, id, nonce, upstream.Query().Get("code_challenge"), state, cookie
}

// A bootstrap URL is not a browser proof, for either MCP or first-party login.
func TestFederatedLoginBootstrapCannotTransferBrowsers(t *testing.T) {
	t.Parallel()
	for _, firstParty := range []bool{false, true} {
		for _, wrongCookie := range []bool{false, true} {
			t.Run(fmt.Sprintf("firstparty=%t/wrongcookie=%t", firstParty, wrongCookie), func(t *testing.T) {
				t.Parallel()
				ctx, f := newFederationLoginFixture(t, true)
				query := url.Values{"response_type": {"code"}, "client_id": {f.downstreamClientID}, "redirect_uri": {"http://127.0.0.1/callback"}, "state": {"downstream-state"}, "code_challenge": {"downstream-pkce"}, "code_challenge_method": {"S256"}}
				route := chi.NewRouteContext()
				route.URLParams.Add("mcpSlug", f.toolsetSlug)
				req := httptest.NewRequest(http.MethodGet, "/mcp/"+f.toolsetSlug+"/authorize?"+query.Encode(), nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
				start := httptest.NewRecorder()
				var err error
				if firstParty {
					err = f.ti.service.HandleFirstPartyConnect(start, req)
				} else {
					err = f.ti.service.HandleAuthorize(start, req)
				}
				require.NoError(t, err)
				cookies := start.Result().Cookies()
				require.Len(t, cookies, 1, "bind at authorize, not at the transferable callback URL")
				victim := httptest.NewRequest(http.MethodGet, start.Header().Get("Location"), nil).WithContext(ctx)
				if wrongCookie {
					cookie := *cookies[0]
					cookie.Value = "victim-browser"
					victim.AddCookie(&cookie)
				}
				response := httptest.NewRecorder()
				err = f.ti.service.HandleIDPCallback(response, victim)
				if firstParty {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					failure := assertFederationErrorRedirect(t, response)
					require.Equal(t, "access_denied", failure.Query().Get("error"))
				}
				require.NotContains(t, response.Header().Get("Location"), f.provider.URL)
				require.Zero(t, f.provider.exchangeCount())
			})
		}
	}
}
