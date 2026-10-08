package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := harness.serve(t, tc.method, "auth.example.com", tc.path, tc.form)
			require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		})
	}
}

// authenticationHostSharedMetadata is the RFC 8414 document a shared
// authorization server serves on the authentication host, with every field
// that document must omit.
type authenticationHostSharedMetadata struct {
	Issuer                          string    `json:"issuer"`
	TokenEndpoint                   string    `json:"token_endpoint"`
	RevocationEndpoint              string    `json:"revocation_endpoint"`
	GrantTypes                      []string  `json:"grant_types_supported"`
	TokenEndpointAuthMethods        []string  `json:"token_endpoint_auth_methods_supported"`
	ResponseTypes                   *[]string `json:"response_types_supported"`
	AuthorizationEndpoint           *string   `json:"authorization_endpoint"`
	RegistrationEndpoint            *string   `json:"registration_endpoint"`
	TokenEndpointAuthSigningAlgs    *[]string `json:"token_endpoint_auth_signing_alg_values_supported"`
	GrantProfiles                   []string  `json:"authorization_grant_profiles_supported"`
	CodeChallengeMethods            []string  `json:"code_challenge_methods_supported"`
	RefreshTokenExpirationTypes     []string  `json:"refresh_token_expiration_types_supported"`
	AuthorizationResponseIss        *bool     `json:"authorization_response_iss_parameter_supported"`
	ClientIDMetadataDocumentSupport *bool     `json:"client_id_metadata_document_supported"`
}

// On the authentication host, metadata advertises the token and revocation
// endpoints, the workload grant, and no client authentication, at both
// metadata locations, even for an issuer with an ID-JAG trust link.
func TestAuthenticationHostSharedAuthorizationServer_MetadataAdvertisesWorkloadGrantOnly(t *testing.T) {
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
			require.Equal(t, issuerURL, metadata.Issuer)
			require.Equal(t, issuerURL+"/token", metadata.TokenEndpoint)
			require.Equal(t, issuerURL+"/revoke", metadata.RevocationEndpoint)
			require.Equal(t, []string{oauthwire.GrantTypeJWTBearer}, metadata.GrantTypes)
			require.Equal(t, []string{oauthwire.AuthMethodNone}, metadata.TokenEndpointAuthMethods)
			require.NotNil(t, metadata.ResponseTypes, "RFC 8414 requires response_types_supported")
			require.Empty(t, *metadata.ResponseTypes)
			require.Nil(t, metadata.AuthorizationEndpoint)
			require.Nil(t, metadata.RegistrationEndpoint)
			require.Nil(t, metadata.TokenEndpointAuthSigningAlgs)
			require.Empty(t, metadata.GrantProfiles)
			require.Empty(t, metadata.CodeChallengeMethods)
			require.Empty(t, metadata.RefreshTokenExpirationTypes)
			require.Nil(t, metadata.AuthorizationResponseIss)
			require.Nil(t, metadata.ClientIDMetadataDocumentSupport)
		})
	}
}

// The authentication host serves no authorization, consent, or registration
// endpoint for a shared authorization server; a request that fell through to
// the main mux would answer 418 instead of 404.
func TestAuthenticationHostSharedAuthorizationServer_BrowserFlowRoutesNotServed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	issuerID := f.issuer.ID.String()
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), authenticationHostSharedIssuerURL(issuerID))

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/authorize"},
		{method: http.MethodPost, path: "/register"},
		{method: http.MethodGet, path: "/connect"},
		{method: http.MethodPost, path: "/connect"},
		{method: http.MethodPost, path: "/connect/remote-session"},
		{method: http.MethodPost, path: "/connect/mcp"},
		{method: http.MethodDelete, path: "/connect/mcp"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()

			w := harness.serve(t, tc.method, "auth.example.com", "/oauth/usi/"+issuerID+tc.path, url.Values{})
			require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		})
	}
}

// The token endpoint on the authentication host refuses the grants the
// browser flows it does not serve would start.
func TestAuthenticationHostSharedAuthorizationServer_BrowserGrantsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	issuerID := f.issuer.ID.String()
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), authenticationHostSharedIssuerURL(issuerID))

	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{name: "authorization code", form: url.Values{oauthwire.ParamGrantType: {oauthwire.GrantTypeAuthorizationCode}, oauthwire.ParamCode: {"code"}, oauthwire.ParamClientID: {f.client.ClientID}}},
		{name: "refresh token", form: url.Values{oauthwire.ParamGrantType: {oauthwire.GrantTypeRefreshToken}, oauthwire.ParamRefreshToken: {"refresh"}, oauthwire.ParamClientID: {f.client.ClientID}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := harness.serve(t, http.MethodPost, "auth.example.com", "/oauth/usi/"+issuerID+"/token", tc.form)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Equal(t, "unsupported_grant_type", decodeSharedTokenResponse(t, w).Error)
		})
	}
}

// The ID-JAG exchange stays refused on the authentication host for a shared
// authorization server, as it is for per-endpoint ones.
func TestAuthenticationHostSharedAuthorizationServer_IDJAGExchangeRefused(t *testing.T) {
	t.Parallel()

	ctx, ti, _, f := sharedIDJAGFixture(t)
	harness := newAuthenticationHostHarness(t, ti)
	issuerID := f.issuer.ID.String()
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), authenticationHostSharedIssuerURL(issuerID))

	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamClientID:  {f.client.ClientID},
		oauthwire.ParamAssertion: {"header.payload.signature"},
		oauthwire.ParamResource:  {f.resource(ti, f.slugA)},
	}
	w := harness.serve(t, http.MethodPost, "auth.example.com", "/oauth/usi/"+issuerID+"/token", form)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "unsupported_grant_type", decodeSharedTokenResponse(t, w).Error)
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
// authentication host keep discovering the per-endpoint authorization server,
// which still serves browser sign-in.
func TestAuthenticationHostSharedAuthorizationServer_ProtectedResourceMetadataKeepsPerEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	newAuthenticationHostHarness(t, ti)
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), authenticationHostSharedIssuerURL(f.issuer.ID.String()))

	require.Equal(t, []string{f.resource(ti, f.slugA)}, protectedResourceAuthorizationServers(t, ctx, ti, f.slugA))
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
