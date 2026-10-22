package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// An upstream login names the consent page it returns to only when that page
// is not the per-endpoint one on the resource origin, which remote sessions
// rebuild and revalidate themselves.
func TestRemoteLoginConsentURL(t *testing.T) {
	t.Parallel()

	const (
		serverURL          = "https://gram.example"
		authenticationHost = "https://id.gram.example"
		stateID            = "state-1"
	)
	sharedIssuerID := uuid.New()
	sharedIssuer := serverURL + "/oauth/usi/" + sharedIssuerID.String()

	for _, tc := range []struct {
		name                  string
		authenticationHost    string
		useAuthenticationHost bool
		customDomain          bool
		shared                bool
		mintOrigin            string
		want                  string
	}{
		{name: "not opted in", authenticationHost: authenticationHost, mintOrigin: serverURL, want: ""},
		{name: "opted in", authenticationHost: authenticationHost, useAuthenticationHost: true, mintOrigin: serverURL, want: authenticationHost + "/mcp/billing/connect?state=" + stateID},
		{name: "opted in on a custom domain", authenticationHost: authenticationHost, useAuthenticationHost: true, customDomain: true, mintOrigin: "https://mcp.customer.example", want: ""},
		{name: "opted in on an extra platform host", authenticationHost: authenticationHost, useAuthenticationHost: true, mintOrigin: "https://app.other.example", want: ""},
		{name: "opted in over a private network", authenticationHost: authenticationHost, useAuthenticationHost: true, mintOrigin: "https://private.customer.internal", want: ""},
		{name: "opted in with the authentication host disabled", authenticationHost: "", useAuthenticationHost: true, mintOrigin: serverURL, want: ""},
		{name: "shared authorization server", authenticationHost: authenticationHost, shared: true, mintOrigin: serverURL, want: sharedIssuer + "/connect?state=" + stateID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := url.Parse(serverURL)
			require.NoError(t, err)
			service := &Service{
				aiCatalog: aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())),
				serverURL: parsed, authenticationHostBaseURL: tc.authenticationHost}
			endpoint := &ResolvedMcpEndpoint{RouteBase: mcproute.MCP, Slug: "billing", useAuthenticationHost: tc.useAuthenticationHost, CustomDomainID: uuid.NullUUID{UUID: uuid.New(), Valid: tc.customDomain}}
			if tc.shared {
				endpoint.sharedAuthorizationServer = &sharedAuthorizationServer{issuerID: sharedIssuerID, issuer: sharedIssuer}
				endpoint.sharedResource = &sharedResource{origin: requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: tc.mintOrigin}}
			}
			state := AuthnChallengeState{ID: stateID, Endpoint: EndpointRef{BaseURL: tc.mintOrigin}}

			got, err := service.remoteLoginConsentURL(endpoint, state)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// A flow started on the MCP host for an issuer that opts in to the
// authentication host moves there, and a request already on the
// authentication host never redirects, even when its base URLs differ in form.
func TestRedirectToAuthorizationServer(t *testing.T) {
	t.Parallel()

	const (
		serverURL          = "https://gram.example"
		authenticationHost = "https://id.gram.example"
		path               = "/mcp/billing/authorize?client_id=client"
	)

	for _, tc := range []struct {
		name               string
		arrivedOn          string
		wantRedirectTarget string
	}{
		{name: "MCP host", arrivedOn: "", wantRedirectTarget: authenticationHost + path},
		{name: "authentication host", arrivedOn: authenticationHost, wantRedirectTarget: ""},
		{name: "authentication host with a trailing slash", arrivedOn: authenticationHost + "/", wantRedirectTarget: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := url.Parse(serverURL)
			require.NoError(t, err)
			service := &Service{
				aiCatalog: aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())),
				serverURL: parsed, authenticationHostBaseURL: authenticationHost}
			endpoint := &ResolvedMcpEndpoint{RouteBase: mcproute.MCP, Slug: "billing", useAuthenticationHost: true}

			ctx := requestorigin.WithContext(t.Context(), requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: serverURL})
			if tc.arrivedOn != "" {
				ctx = context.WithValue(ctx, authenticationHostContextKey{}, tc.arrivedOn)
			}
			w := httptest.NewRecorder()
			redirected := service.redirectToAuthorizationServer(w, httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil), endpoint)

			require.Equal(t, tc.wantRedirectTarget != "", redirected)
			require.Equal(t, tc.wantRedirectTarget, w.Header().Get("Location"))
		})
	}
}
