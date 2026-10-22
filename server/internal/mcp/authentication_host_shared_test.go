package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// authenticationHostSharedIssuerURL is the shared authorization server issuer
// of issuerID when it is pinned to the authentication host.
func authenticationHostSharedIssuerURL(issuerID string) string {
	return testAuthenticationHostURL + "/oauth/usi/" + issuerID
}

// pinWorkloadIssuerToAuthenticationHost switches the workload fixture's
// issuer to shared mode with its issuer pinned to the authentication host,
// leaving use_authentication_host off, and returns the pinned issuer.
func (f workloadGrantFixture) pinWorkloadIssuerToAuthenticationHost(t *testing.T) string {
	t.Helper()

	issuerURL := authenticationHostSharedIssuerURL(f.fx.target.UserSessionIssuerID.String())
	rows, err := testrepo.New(f.ti.conn).SetUserSessionIssuerAuthorizationServerModeFixture(t.Context(), testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{
		AuthorizationServerMode: string(authserver.ModeShared),
		PinnedIssuerUrl:         conv.ToPGText(issuerURL),
		IssuerID:                f.fx.target.UserSessionIssuerID,
		OrganizationID:          f.fx.orgID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	return issuerURL
}

// A workload grant sent to the token endpoint on the authentication host, for
// an issuer pinned there, mints a session bound to the resource it names. The
// assertion may name the issuer or the token URL on the authentication host.
func TestAuthenticationHostSharedAuthorizationServer_WorkloadGrantBindsResource(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{"", "/token"} {
		t.Run("audience"+suffix, func(t *testing.T) {
			t.Parallel()

			f := newWorkloadGrantFixture(t)
			harness := newAuthenticationHostHarness(t, f.ti)
			issuerURL := f.pinWorkloadIssuerToAuthenticationHost(t)

			form := url.Values{
				oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
				oauthwire.ParamAssertion: {f.assertion(t, issuerURL+suffix)},
				oauthwire.ParamResource:  {f.resource},
			}
			w := harness.serve(t, http.MethodPost, "auth.example.com", "/oauth/usi/"+f.fx.target.UserSessionIssuerID.String()+"/token", form)
			f.requireWorkloadSession(t, w, issuerURL)
			require.False(t, *harness.passedThrough)

			claims := accessTokenClaims(t, w.Body.Bytes())
			require.Equal(t, []any{f.resource}, claims["aud"])
		})
	}
}

// A workload grant naming no resource, as Claude Tag sends when its Resource
// field is left empty, mints a session on the authentication host for all of
// the issuer's MCP servers, which the MCP side admits.
func TestAuthenticationHostSharedAuthorizationServer_WorkloadGrantWithoutResourceCoversIssuer(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	harness := newAuthenticationHostHarness(t, f.ti)
	issuerURL := f.pinWorkloadIssuerToAuthenticationHost(t)

	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamAssertion: {f.assertion(t, issuerURL)},
	}
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/oauth/usi/"+f.fx.target.UserSessionIssuerID.String()+"/token", form)
	f.requireWorkloadSession(t, w, issuerURL)
	require.False(t, *harness.passedThrough)

	claims := accessTokenClaims(t, w.Body.Bytes())
	require.Equal(t, []any{urn.NewUserSessionIssuerMCPServers(f.fx.target.UserSessionIssuerID).String()}, claims["aud"])
}

// A workload assertion addressed to the issuer's server URL form is refused on
// the authentication host: the accepted audiences are the issuer pinned there
// and its token URL.
func TestAuthenticationHostSharedAuthorizationServer_WorkloadGrantRefusesServerURLAudience(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	harness := newAuthenticationHostHarness(t, f.ti)
	f.pinWorkloadIssuerToAuthenticationHost(t)
	issuerID := f.fx.target.UserSessionIssuerID.String()
	serverURLIssuer := f.ti.serverURL.String() + "/oauth/usi/" + issuerID

	for _, audience := range []string{serverURLIssuer, serverURLIssuer + "/token"} {
		form := url.Values{
			oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
			oauthwire.ParamAssertion: {f.assertion(t, audience)},
			oauthwire.ParamResource:  {f.resource},
		}
		requireWorkloadGrantRefused(t, harness.serve(t, http.MethodPost, "auth.example.com", "/oauth/usi/"+issuerID+"/token", form))
	}
	require.Empty(t, f.workloadSessions(t))
}

// An issuer pinned to the authentication host is served there alone: its
// shared routes answer 404 on the server URL's host.
func TestAuthenticationHostSharedAuthorizationServer_NotServedOnServerURL(t *testing.T) {
	t.Parallel()

	f := newWorkloadGrantFixture(t)
	newAuthenticationHostHarness(t, f.ti)
	issuerURL := f.pinWorkloadIssuerToAuthenticationHost(t)
	issuerID := f.fx.target.UserSessionIssuerID.String()

	err := f.ti.service.HandleSharedAuthorizationServerMetadata(httptest.NewRecorder(), sharedRequest(t.Context(), http.MethodGet, "/.well-known/oauth-authorization-server/oauth/usi/"+issuerID, issuerID, nil))
	requireNotFound(t, err)

	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamAssertion: {f.assertion(t, issuerURL)},
		oauthwire.ParamResource:  {f.resource},
	}
	err = f.ti.service.HandleSharedToken(httptest.NewRecorder(), sharedRequest(t.Context(), http.MethodPost, "/oauth/usi/"+issuerID+"/token", issuerID, strings.NewReader(form.Encode())))
	requireNotFound(t, err)
	require.Empty(t, f.workloadSessions(t))

	err = f.ti.service.HandleSharedAuthorize(httptest.NewRecorder(), sharedRequest(t.Context(), http.MethodGet, "/oauth/usi/"+issuerID+"/authorize?"+url.Values{oauthwire.ParamResource: {f.resource}}.Encode(), issuerID, nil))
	requireNotFound(t, err)
}

// A shared authorization server whose issuer is not on the authentication host
// answers 404 there, as an unknown issuer does. A request that fell through to
// the main mux would answer 418 instead.
func TestAuthenticationHostSharedAuthorizationServer_OtherIssuersNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	issuerID := f.issuer.ID.String()

	// The issuer is still served on its own host.
	require.Equal(t, []string{f.issuerURL}, protectedResourceAuthorizationServers(t, ctx, ti, f.slugA))

	for _, tc := range []struct {
		name   string
		method string
		path   string
		form   url.Values
	}{
		{name: "metadata", method: http.MethodGet, path: "/.well-known/oauth-authorization-server/oauth/usi/" + issuerID, form: nil},
		{name: "appended metadata", method: http.MethodGet, path: "/oauth/usi/" + issuerID + "/.well-known/oauth-authorization-server", form: nil},
		{name: "token", method: http.MethodPost, path: "/oauth/usi/" + issuerID + "/token", form: url.Values{oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer}, oauthwire.ParamAssertion: {"header.payload.signature"}, oauthwire.ParamResource: {f.resource(ti, f.slugA)}}},
		{name: "revoke", method: http.MethodPost, path: "/oauth/usi/" + issuerID + "/revoke", form: url.Values{oauthwire.ParamToken: {"opaque"}, oauthwire.ParamClientID: {f.client.ClientID}}},
		{name: "register", method: http.MethodPost, path: "/oauth/usi/" + issuerID + "/register", form: url.Values{}},
		{name: "authorize", method: http.MethodGet, path: "/oauth/usi/" + issuerID + "/authorize?" + url.Values{oauthwire.ParamResource: {f.resource(ti, f.slugA)}}.Encode(), form: nil},
		{name: "consent", method: http.MethodGet, path: "/oauth/usi/" + issuerID + "/connect?state=" + uuid.NewString(), form: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := harness.serve(t, tc.method, "auth.example.com", tc.path, tc.form)
			require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		})
	}
}

// authenticationHostSharedMetadata is the part of the RFC 8414 document a
// shared authorization server serves on the authentication host that names
// its endpoints and grants.
type authenticationHostSharedMetadata struct {
	Issuer                   string   `json:"issuer"`
	AuthorizationEndpoint    string   `json:"authorization_endpoint"`
	TokenEndpoint            string   `json:"token_endpoint"`
	RegistrationEndpoint     string   `json:"registration_endpoint"`
	RevocationEndpoint       string   `json:"revocation_endpoint"`
	TokenEndpointAuthMethods []string `json:"token_endpoint_auth_methods_supported"`
	ResponseTypes            []string `json:"response_types_supported"`
	GrantTypes               []string `json:"grant_types_supported"`
	GrantProfiles            []string `json:"authorization_grant_profiles_supported"`
}

// On the authentication host, metadata advertises browser sign-in alongside
// the ID-JAG exchange of an issuer with a trust link, at both metadata
// locations, with every endpoint on the authentication host.
func TestAuthenticationHostSharedAuthorizationServer_MetadataAdvertisesBrowserSignIn(t *testing.T) {
	t.Parallel()

	ctx, ti, _, f := sharedIDJAGFixture(t)
	harness := newAuthenticationHostHarness(t, ti)
	issuerID := f.issuer.ID.String()
	issuerURL := authenticationHostSharedIssuerURL(issuerID)
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), issuerURL)

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "path insertion", path: "/.well-known/oauth-authorization-server/oauth/usi/" + issuerID},
		{name: "path appended", path: "/oauth/usi/" + issuerID + "/.well-known/oauth-authorization-server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := harness.serve(t, http.MethodGet, "auth.example.com", tc.path, nil)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			var metadata authenticationHostSharedMetadata
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
			require.Equal(t, authenticationHostSharedMetadata{
				Issuer:                   issuerURL,
				AuthorizationEndpoint:    issuerURL + "/authorize",
				TokenEndpoint:            issuerURL + "/token",
				RegistrationEndpoint:     issuerURL + "/register",
				RevocationEndpoint:       issuerURL + "/revoke",
				TokenEndpointAuthMethods: usersessions.SupportedAuthMethods,
				ResponseTypes:            []string{"code"},
				GrantTypes:               []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken, oauthwire.GrantTypeClientCredentials, oauthwire.GrantTypeJWTBearer},
				GrantProfiles:            []string{oauthwire.GrantProfileIDJAG},
			}, metadata)
		})
	}
}

// A person signs in through a shared authorization server on the
// authentication host: authorization, consent, and the code and refresh grants
// all run there, for an issuer pinned there without opting in to
// use_authentication_host.
func TestAuthenticationHostSharedAuthorizationServer_BrowserSignIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	issuerID := f.issuer.ID.String()
	issuerURL := authenticationHostSharedIssuerURL(issuerID)
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), issuerURL)
	resource := f.resource(ti, f.slugA)
	verifier := pkceVerifier(t)

	query := url.Values{
		"response_type":         {"code"},
		oauthwire.ParamClientID: {f.client.ClientID},
		"redirect_uri":          {sharedTestRedirectURI},
		oauthwire.ParamState:    {"client-state"},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
		oauthwire.ParamResource: {resource},
	}
	authorized := harness.serve(t, http.MethodGet, "auth.example.com", "/oauth/usi/"+issuerID+"/authorize?"+query.Encode(), nil)
	require.Equal(t, http.StatusFound, authorized.Code, authorized.Body.String())
	consentURL, err := url.Parse(authorized.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, issuerURL+"/connect", consentURL.Scheme+"://"+consentURL.Host+consentURL.Path)
	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+consentURL.Query().Get("state"))
	require.NoError(t, err)

	page := harness.serve(t, http.MethodGet, "auth.example.com", consentURL.RequestURI(), nil)
	require.Equal(t, http.StatusOK, page.Code, page.Body.String())

	consented := harness.serve(t, http.MethodPost, "auth.example.com", "/oauth/usi/"+issuerID+"/connect", url.Values{"state": {stored.ID}, "csrf_token": {stored.CSRFToken}, "action": {"approve"}})
	require.Equal(t, http.StatusSeeOther, consented.Code, consented.Body.String())
	redirect, err := url.Parse(consented.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, issuerURL, redirect.Query().Get("iss"))
	code := redirect.Query().Get(oauthwire.ParamCode)
	require.NotEmpty(t, code, redirect.String())

	exchanged := harness.serve(t, http.MethodPost, "auth.example.com", "/oauth/usi/"+issuerID+"/token", url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeAuthorizationCode},
		oauthwire.ParamCode:      {code},
		"redirect_uri":           {sharedTestRedirectURI},
		oauthwire.ParamClientID:  {f.client.ClientID},
		"code_verifier":          {verifier},
	})
	require.Equal(t, http.StatusOK, exchanged.Code, exchanged.Body.String())
	claims := accessTokenClaims(t, exchanged.Body.Bytes())
	require.Equal(t, issuerURL, claims["iss"])
	require.Equal(t, []any{resource}, claims["aud"])

	refreshed := harness.serve(t, http.MethodPost, "auth.example.com", "/oauth/usi/"+issuerID+"/token", url.Values{
		oauthwire.ParamGrantType:    {oauthwire.GrantTypeRefreshToken},
		oauthwire.ParamRefreshToken: {decodeSharedTokenResponse(t, exchanged).RefreshToken},
		oauthwire.ParamClientID:     {f.client.ClientID},
	})
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	require.False(t, *harness.passedThrough)
}

// Dynamic client registration on the authentication host registers the client
// with the issuer pinned there.
func TestAuthenticationHostSharedAuthorizationServer_Registers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	issuerID := f.issuer.ID.String()
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), authenticationHostSharedIssuerURL(issuerID))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/oauth/usi/"+issuerID+"/register", strings.NewReader(`{"client_name":"authentication host dcr","redirect_uris":["`+sharedTestRedirectURI+`"],"token_endpoint_auth_method":"none"}`))
	req.Header.Set("Content-Type", "application/json")
	registered := harness.serveRequest("auth.example.com", req)
	require.Equal(t, http.StatusCreated, registered.Code, registered.Body.String())
	var registration struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(registered.Body.Bytes(), &registration))
	_, err := usersessions_repo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: f.issuer.ID,
		ClientID:            registration.ClientID,
	})
	require.NoError(t, err)
}

// The ID-JAG exchange on the authentication host accepts an assertion
// addressed to the issuer pinned there.
func TestAuthenticationHostSharedAuthorizationServer_IDJAGExchange(t *testing.T) {
	t.Parallel()

	ctx, ti, signer, f := sharedIDJAGFixture(t)
	harness := newAuthenticationHostHarness(t, ti)
	issuerID := f.issuer.ID.String()
	issuerURL := authenticationHostSharedIssuerURL(issuerID)
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), issuerURL)
	resource := f.resource(ti, f.slugA)

	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamClientID:  {f.client.ClientID},
		oauthwire.ParamAssertion: {signer.sign(t, sharedTestUpstreamIssuer, issuerURL, resource, f.client.ClientID, mockidp.MockUserEmail, uuid.NewString())},
		oauthwire.ParamResource:  {resource},
	}
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/oauth/usi/"+issuerID+"/token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	claims := accessTokenClaims(t, w.Body.Bytes())
	require.Equal(t, issuerURL, claims["iss"])
	require.Equal(t, []any{resource}, claims["aud"])
}

// The revocation endpoint on the authentication host revokes the issuer's
// tokens for its clients: a refresh token minted while the shared
// authorization server was on the server URL is revoked there after the issuer
// is pinned to the authentication host.
func TestAuthenticationHostSharedAuthorizationServer_Revokes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	issuerID := f.issuer.ID.String()
	refreshToken := sharedTokens(t, ctx, ti, f, f.resource(ti, f.slugA)).RefreshToken
	require.NotEmpty(t, refreshToken)

	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), authenticationHostSharedIssuerURL(issuerID))
	revoked := harness.serve(t, http.MethodPost, "auth.example.com", "/oauth/usi/"+issuerID+"/revoke", url.Values{
		oauthwire.ParamToken:         {refreshToken},
		oauthwire.ParamTokenTypeHint: {oauthwire.ParamRefreshToken},
		oauthwire.ParamClientID:      {f.client.ClientID},
	})
	require.Equal(t, http.StatusOK, revoked.Code, revoked.Body.String())

	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), "")
	refreshed := sharedToken(t, ctx, ti, issuerID, url.Values{
		oauthwire.ParamGrantType:    {oauthwire.GrantTypeRefreshToken},
		oauthwire.ParamRefreshToken: {refreshToken},
		oauthwire.ParamClientID:     {f.client.ClientID},
	})
	require.Equal(t, http.StatusBadRequest, refreshed.Code, refreshed.Body.String())
	require.Equal(t, "invalid_grant", decodeSharedTokenResponse(t, refreshed).Error)
}

// MCP clients of an issuer whose shared authorization server is on the
// authentication host discover it there.
func TestAuthenticationHostSharedAuthorizationServer_ProtectedResourceMetadataNamesAuthenticationHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	newAuthenticationHostHarness(t, ti)
	issuerURL := authenticationHostSharedIssuerURL(f.issuer.ID.String())
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), issuerURL)

	require.Equal(t, []string{issuerURL}, protectedResourceAuthorizationServers(t, ctx, ti, f.slugA))
}

// An unpinned issuer that opts in to the authentication host derives its
// shared authorization server there.
func TestAuthenticationHostSharedAuthorizationServer_OptedInIssuerDerivesAuthenticationHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	useAuthenticationHost(t, ctx, ti, f.issuer.OrganizationID.String, f.issuer.ID)
	issuerID := f.issuer.ID.String()

	w := harness.serve(t, http.MethodGet, "auth.example.com", "/.well-known/oauth-authorization-server/oauth/usi/"+issuerID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var metadata authenticationHostSharedMetadata
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, authenticationHostSharedIssuerURL(issuerID), metadata.Issuer)

	err := ti.service.HandleSharedAuthorizationServerMetadata(httptest.NewRecorder(), sharedRequest(ctx, http.MethodGet, "/.well-known/oauth-authorization-server/oauth/usi/"+issuerID, issuerID, nil))
	requireNotFound(t, err)
}
