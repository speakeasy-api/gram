package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/stretchr/testify/require"
)

// perClientFederation is a started federated login; every federated login uses
// the per-client callback whether or not the provider advertises RFC 9207 iss.
type perClientFederation struct {
	f                     *federationLoginFixture
	id, nonce, challenge  string
	cookie                *http.Cookie
	callbackPath          string
	perClientCallbackBase string
}

// idpCallbackRequest builds an IdP callback as the router would deliver it,
// with clientID set only for the per-client route.
func idpCallbackRequest(ctx context.Context, base, path, routeClientID string, query url.Values, cookie *http.Cookie) *http.Request {
	if routeClientID != "" {
		route := chi.NewRouteContext()
		route.URLParams.Add("clientID", routeClientID)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, route)
	}
	req := httptest.NewRequest(http.MethodGet, base+path+"?"+query.Encode(), nil).WithContext(ctx)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

// routeIDPCallback sets the clientID route param the router extracts from a per-client callback path.
func routeIDPCallback(req *http.Request) *http.Request {
	clientID, ok := strings.CutPrefix(req.URL.Path, "/mcp/idp_callback/")
	if !ok {
		return req
	}
	route := chi.NewRouteContext()
	route.URLParams.Add("clientID", clientID)
	ctx := req.Context()
	if req.URL.IsAbs() {
		ctx = requestorigin.WithContext(ctx, requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: req.URL.Scheme + "://" + req.URL.Host})
	}
	return req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
}

func beginPerClientFederation(t *testing.T, advertised bool) (context.Context, *perClientFederation) {
	t.Helper()
	ctx, f := newFederationLoginFixture(t, true)
	f.provider.mu.Lock()
	f.provider.unsupportedResponseIssuer = !advertised
	f.provider.mu.Unlock()
	setIssuerResponseIssColumn(t, ctx, f, advertised)
	return ctx, beginFederationFromFixture(t, ctx, f)
}

func beginFederationFromFixture(t *testing.T, ctx context.Context, f *federationLoginFixture) *perClientFederation {
	t.Helper()
	ti := f.ti
	query := url.Values{"response_type": {"code"}, "client_id": {f.downstreamClientID}, "redirect_uri": {"http://127.0.0.1/callback"}, "state": {"downstream-state"}, "code_challenge": {"downstream-pkce"}, "code_challenge_method": {"S256"}}
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", f.toolsetSlug)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+f.toolsetSlug+"/authorize?"+query.Encode(), nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
	start := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(start, req))
	require.Equal(t, http.StatusFound, start.Code)
	bootstrap, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	cookies := start.Result().Cookies()
	require.Len(t, cookies, 1)

	callbackPath := f.callbackPath()
	routeClientID := f.clientID.String()
	require.Equal(t, callbackPath, bootstrap.Path, "advertised iss does not change the redirect URI")
	begin := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleIDPCallback(begin, idpCallbackRequest(ctx, ti.serverURL.String(), bootstrap.Path, routeClientID, bootstrap.Query(), cookies[0])))
	upstream, err := url.Parse(begin.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, f.provider.URL+"/authorize", upstream.Scheme+"://"+upstream.Host+upstream.Path)
	require.Equal(t, ti.serverURL.String()+callbackPath, upstream.Query().Get("redirect_uri"))
	return &perClientFederation{f: f, id: upstream.Query().Get("state"), nonce: upstream.Query().Get("nonce"), challenge: upstream.Query().Get("code_challenge"), cookie: cookies[0], callbackPath: callbackPath, perClientCallbackBase: ti.serverURL.String()}
}

// setIssuerResponseIssColumn sets the operator-managed RFC 9207 column on the trusted issuer.
func setIssuerResponseIssColumn(t *testing.T, ctx context.Context, f *federationLoginFixture, supported bool) {
	t.Helper()
	_, err := remotesessionsrepo.New(f.ti.conn).UpdateOrganizationRemoteSessionIssuer(ctx, remotesessionsrepo.UpdateOrganizationRemoteSessionIssuerParams{
		ID: f.remoteIssuerID, OrganizationID: conv.ToPGText(f.organizationID), AuthorizationResponseIssParameterSupported: pgtype.Bool{Bool: supported, Valid: true},
	})
	require.NoError(t, err)
}

// requireUnconsumedRejection asserts a 401 that left the callback state usable.
func requireUnconsumedRejection(t *testing.T, ctx context.Context, p *perClientFederation, err error) {
	t.Helper()
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
	_, err = p.f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+p.id)
	require.NoError(t, err, "a misrouted callback must not consume the state")
	require.Zero(t, p.f.provider.exchangeCount())
}

func (p *perClientFederation) issueCode(t *testing.T) {
	t.Helper()
	p.f.provider.issueCode(t, "one-use-code", federationToken{challenge: p.challenge, nonce: p.nonce, email: mockidp.MockUserEmail, issuer: p.f.provider.URL, verified: true, secret: "selected-secret"})
}

func (p *perClientFederation) callback(ctx context.Context, path, routeClientID string, query url.Values) *http.Request {
	return idpCallbackRequest(ctx, p.perClientCallbackBase, path, routeClientID, query, p.cookie)
}

func requireFederatedConsentRedirect(t *testing.T, result *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusFound, result.Code)
	consent, err := url.Parse(result.Header().Get("Location"))
	require.NoError(t, err)
	require.Contains(t, consent.Path, "/connect")
	require.NotEmpty(t, consent.Query().Get("state"))
}

// A provider without RFC 9207 iss completes login on its per-client callback, with
// or without iss; a present iss must still match.
func TestFederatedLoginPerClientCallbackSucceeds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		withIssuer bool
	}{
		{name: "without iss", withIssuer: false},
		{name: "with matching iss", withIssuer: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, p := beginPerClientFederation(t, false)
			p.issueCode(t)
			query := url.Values{"state": {p.id}, "code": {"one-use-code"}}
			if test.withIssuer {
				query.Set("iss", p.f.provider.URL)
			}
			result := httptest.NewRecorder()
			require.NoError(t, p.f.ti.service.HandleIDPCallback(result, p.callback(ctx, p.callbackPath, p.f.clientID.String(), query)))
			requireFederatedConsentRedirect(t, result)
			require.Equal(t, 1, p.f.provider.exchangeCount())
		})
	}
}

// Only an omitted iss is tolerated; a present one must be a single match.
func TestFederatedLoginPerClientCallbackRejectsWrongIssuer(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		iss  []string
	}{
		{name: "other issuer", iss: []string{"https://other.example.test"}},
		{name: "present empty", iss: []string{""}},
		{name: "repeated", iss: []string{"provider", "provider"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, p := beginPerClientFederation(t, false)
			p.issueCode(t)
			query := url.Values{"state": {p.id}, "code": {"one-use-code"}}
			for _, iss := range test.iss {
				if iss == "provider" {
					iss = p.f.provider.URL
				}
				query.Add("iss", iss)
			}
			result := httptest.NewRecorder()
			require.NoError(t, p.f.ti.service.HandleIDPCallback(result, p.callback(ctx, p.callbackPath, p.f.clientID.String(), query)))
			failure := assertFederationErrorRedirect(t, result)
			require.Equal(t, "access_denied", failure.Query().Get("error"))
			require.Zero(t, p.f.provider.exchangeCount())
		})
	}
}

// The mounted route extracts {clientID} and completes a real per-client login.
func TestFederatedLoginPerClientCallbackThroughMountedRoute(t *testing.T) {
	t.Parallel()
	_, p := beginPerClientFederation(t, false)
	p.issueCode(t)
	handler, canonical, _ := newPlatformHostMux(t, p.f.ti)
	header := http.Header{"Sec-Fetch-Site": {"cross-site"}, "Sec-Fetch-Mode": {"navigate"}, "Cookie": {p.cookie.Name + "=" + p.cookie.Value}}
	query := url.Values{"state": {p.id}, "code": {"one-use-code"}}
	result := serveOnHost(t, handler, http.MethodGet, canonical.host, p.callbackPath+"?"+query.Encode(), nil, header)
	requireFederatedConsentRedirect(t, result)
	require.Equal(t, 1, p.f.provider.exchangeCount())
}

// A per-client challenge cannot complete on the shared callback route, and the misroute does not spend the state.
func TestFederatedLoginPerClientChallengeRejectsSharedCallback(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/mcp/idp_callback", "slug"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			ctx, p := beginPerClientFederation(t, false)
			p.issueCode(t)
			if path == "slug" {
				path = "/mcp/" + p.f.toolsetSlug + "/idp_callback"
			}
			query := url.Values{"state": {p.id}, "code": {"one-use-code"}}
			requireUnconsumedRejection(t, ctx, p, p.f.ti.service.HandleIDPCallback(httptest.NewRecorder(), p.callback(ctx, path, "", query)))
			result := httptest.NewRecorder()
			require.NoError(t, p.f.ti.service.HandleIDPCallback(result, p.callback(ctx, p.callbackPath, p.f.clientID.String(), query)))
			requireFederatedConsentRedirect(t, result)
		})
	}
}

// A callback naming another client is rejected without spending the state.
func TestFederatedLoginPerClientCallbackRejectsOtherClientID(t *testing.T) {
	t.Parallel()
	ctx, p := beginPerClientFederation(t, false)
	p.issueCode(t)
	query := url.Values{"state": {p.id}, "code": {"one-use-code"}}
	other := uuid.NewString()
	err := p.f.ti.service.HandleIDPCallback(httptest.NewRecorder(), p.callback(ctx, "/mcp/idp_callback/"+other, other, query))
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
	require.Zero(t, p.f.provider.exchangeCount())

	result := httptest.NewRecorder()
	require.NoError(t, p.f.ti.service.HandleIDPCallback(result, p.callback(ctx, p.callbackPath, p.f.clientID.String(), query)))
	requireFederatedConsentRedirect(t, result)
}

// The path must match the recorded callback even when the route names the client.
func TestFederatedLoginPerClientCallbackRejectsOtherRouteBase(t *testing.T) {
	t.Parallel()
	ctx, p := beginPerClientFederation(t, false)
	p.issueCode(t)
	query := url.Values{"state": {p.id}, "code": {"one-use-code"}}
	requireUnconsumedRejection(t, ctx, p, p.f.ti.service.HandleIDPCallback(httptest.NewRecorder(), p.callback(ctx, "/x/mcp/idp_callback/"+p.f.clientID.String(), p.f.clientID.String(), query)))
}

// Non-canonical spellings of the client id never match the recorded callback: 401 through the real mux, state intact.
func TestFederatedLoginPerClientCallbackRejectsNonCanonicalClientIDThroughMux(t *testing.T) {
	t.Parallel()
	_, p := beginPerClientFederation(t, false)
	p.issueCode(t)
	handler, canonical, _ := newPlatformHostMux(t, p.f.ti)
	header := http.Header{"Sec-Fetch-Site": {"cross-site"}, "Sec-Fetch-Mode": {"navigate"}, "Cookie": {p.cookie.Name + "=" + p.cookie.Value}}
	query := url.Values{"state": {p.id}, "code": {"one-use-code"}}
	id := p.f.clientID.String()
	for _, variant := range []string{strings.ToUpper(id), strings.ReplaceAll(id, "-", ""), "%7B" + id + "%7D", "urn:uuid:" + id, id + "%2F"} {
		result := serveOnHost(t, handler, http.MethodGet, canonical.host, "/mcp/idp_callback/"+variant+"?"+query.Encode(), nil, header)
		require.Equal(t, http.StatusUnauthorized, result.Code, variant)
		_, err := p.f.ti.authnChallengeCache.Get(context.Background(), "authnChallenge:"+p.id)
		require.NoError(t, err, "state intact after %s", variant)
		require.Zero(t, p.f.provider.exchangeCount())
	}
	result := serveOnHost(t, handler, http.MethodGet, canonical.host, p.callbackPath+"?"+query.Encode(), nil, header)
	requireFederatedConsentRedirect(t, result)
}

// Flipping RFC 9207 iss advertisement after the login started is configuration drift, never a silent iss downgrade or upgrade.
func TestFederatedLoginResponseIssuerAdvertisementFlipIsConfigDrift(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		advertised bool
	}{
		{name: "column turned on", advertised: false},
		{name: "column turned off", advertised: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, p := beginPerClientFederation(t, test.advertised)
			p.issueCode(t)
			setIssuerResponseIssColumn(t, ctx, p.f, !test.advertised)
			query := url.Values{"state": {p.id}, "code": {"one-use-code"}, "iss": {p.f.provider.URL}}
			result := httptest.NewRecorder()
			require.NoError(t, p.f.ti.service.HandleIDPCallback(result, p.callback(ctx, p.callbackPath, p.f.clientID.String(), query)))
			failure := assertFederationErrorRedirect(t, result)
			require.Equal(t, "server_error", failure.Query().Get("error"))
			require.Zero(t, p.f.provider.exchangeCount())
		})
	}
}

// An operator-set iss column requires iss even when discovery does not advertise it.
func TestFederatedLoginOperatorRequiredResponseIssuer(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		withIss   bool
		wantError string
	}{
		{name: "missing iss", wantError: "access_denied"},
		{name: "matching iss", withIss: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newFederationLoginFixture(t, true)
			f.provider.mu.Lock()
			f.provider.unsupportedResponseIssuer = true
			f.provider.mu.Unlock()
			p := beginFederationFromFixture(t, ctx, f)
			p.issueCode(t)
			query := url.Values{"state": {p.id}, "code": {"one-use-code"}}
			if test.withIss {
				query.Set("iss", f.provider.URL)
			}
			result := httptest.NewRecorder()
			require.NoError(t, f.ti.service.HandleIDPCallback(result, p.callback(ctx, p.callbackPath, f.clientID.String(), query)))
			if test.wantError == "" {
				requireFederatedConsentRedirect(t, result)
				return
			}
			failure := assertFederationErrorRedirect(t, result)
			require.Equal(t, test.wantError, failure.Query().Get("error"))
			require.Zero(t, f.provider.exchangeCount())
		})
	}
}

// Okta omits iss on error redirects too; the decline still reaches the client.
func TestFederatedLoginPerClientCallbackForwardsDeclineWithoutIssuer(t *testing.T) {
	t.Parallel()
	ctx, p := beginPerClientFederation(t, false)
	query := url.Values{"state": {p.id}, "error": {"access_denied"}}
	result := httptest.NewRecorder()
	require.NoError(t, p.f.ti.service.HandleIDPCallback(result, p.callback(ctx, p.callbackPath, p.f.clientID.String(), query)))
	failure := assertFederationErrorRedirect(t, result)
	require.Equal(t, "access_denied", failure.Query().Get("error"))
	require.Equal(t, "Login was declined", failure.Query().Get("error_description"))
	require.Zero(t, p.f.provider.exchangeCount())
}

// An advertising provider also uses the per-client callback, and must return iss there.
func TestFederatedLoginAdvertisedIssuerPerClientCallback(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, iss, wantError string
	}{
		{name: "matching iss", iss: "provider"},
		{name: "missing iss", iss: "", wantError: "access_denied"},
		{name: "wrong iss", iss: "https://other.example.test", wantError: "access_denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, p := beginPerClientFederation(t, true)
			p.issueCode(t)
			query := url.Values{"state": {p.id}, "code": {"one-use-code"}}
			switch test.iss {
			case "":
			case "provider":
				query.Set("iss", p.f.provider.URL)
			default:
				query.Set("iss", test.iss)
			}
			result := httptest.NewRecorder()
			require.NoError(t, p.f.ti.service.HandleIDPCallback(result, p.callback(ctx, p.callbackPath, p.f.clientID.String(), query)))
			if test.wantError == "" {
				requireFederatedConsentRedirect(t, result)
				require.Equal(t, 1, p.f.provider.exchangeCount())
				return
			}
			failure := assertFederationErrorRedirect(t, result)
			require.Equal(t, test.wantError, failure.Query().Get("error"))
			require.Zero(t, p.f.provider.exchangeCount())
		})
	}
}

// A challenge recording the shared callback never completes on any route, and is not spent by trying.
func TestFederatedLoginRejectsSharedCallbackChallenge(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/mcp/idp_callback", "/x/mcp/idp_callback", "slug", "per-client"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			ctx, p := beginPerClientFederation(t, true)
			state, err := p.f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+p.id)
			require.NoError(t, err)
			state.Federation.CallbackURL = p.perClientCallbackBase + "/mcp/idp_callback"
			require.NoError(t, p.f.ti.authnChallengeCache.Store(ctx, state))
			p.issueCode(t)
			query := url.Values{"state": {p.id}, "code": {"one-use-code"}, "iss": {p.f.provider.URL}}
			routeID := ""
			switch path {
			case "per-client":
				path, routeID = p.callbackPath, p.f.clientID.String()
			case "slug":
				path = "/mcp/" + p.f.toolsetSlug + "/idp_callback"
			}
			requireUnconsumedRejection(t, ctx, p, p.f.ti.service.HandleIDPCallback(httptest.NewRecorder(), p.callback(ctx, path, routeID, query)))
		})
	}
}

// A callback on another first-party host must not consume the challenge even
// when its path and client ID match. Forwarding headers cannot repair the host.
func TestFederatedLoginPerClientCallbackRejectsOtherHostThroughMux(t *testing.T) {
	t.Parallel()
	ctx, p := beginPerClientFederation(t, false)
	p.issueCode(t)
	handler, canonical, extra := newPlatformHostMux(t, p.f.ti)
	query := url.Values{"state": {p.id}, "code": {"one-use-code"}}
	headers := http.Header{"Sec-Fetch-Site": {"cross-site"}, "Sec-Fetch-Mode": {"navigate"}, "X-Forwarded-Host": {canonical.host}, "X-Forwarded-Proto": {"https"}}
	result := serveOnHost(t, handler, http.MethodGet, extra.host, p.callbackPath+"?"+query.Encode(), nil, headers)
	require.Equal(t, http.StatusUnauthorized, result.Code)
	_, err := p.f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+p.id)
	require.NoError(t, err, "wrong-host callback must leave state usable")
	require.Zero(t, p.f.provider.exchangeCount())
	headers.Set("Cookie", p.cookie.Name+"="+p.cookie.Value)
	result = serveOnHost(t, handler, http.MethodGet, canonical.host, p.callbackPath+"?"+query.Encode(), nil, headers)
	requireFederatedConsentRedirect(t, result)
}
