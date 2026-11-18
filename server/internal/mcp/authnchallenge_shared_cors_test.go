package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestSharedAuthorizationServer_ConsentHeadersPreserveClientRedirects(t *testing.T) {
	t.Parallel()

	for _, redirectURI := range []string{"http://127.0.0.1:45678/callback", "example-client://oauth/callback"} {
		for _, action := range []string{"approve", "deny"} {
			t.Run(action+"/"+redirectURI, func(t *testing.T) {
				t.Parallel()

				ctx, ti := newTestMCPService(t)
				f := seedSharedIssuer(t, ctx, ti)
				client, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
					UserSessionIssuerID:     f.issuer.ID,
					ClientID:                "redirect-client",
					ClientName:              "Redirect client",
					RedirectUris:            []string{redirectURI},
					TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
				})
				require.NoError(t, err)
				f.client = client

				authorized := sharedAuthorize(t, ctx, ti, f, pkceChallenge(pkceVerifier(t)), f.resource(ti, f.slugA))
				require.Equal(t, http.StatusFound, authorized.Code)
				consentURL, err := url.Parse(authorized.Header().Get("Location"))
				require.NoError(t, err)
				stateID := consentURL.Query().Get(oauthwire.ParamState)
				state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
				require.NoError(t, err)

				handler := middleware.CORSMiddleware("prod", ti.serverURL.String(), nil, nil)(oops.ErrHandle(ti.logger, ti.service.HandleSharedConsent))
				page := httptest.NewRecorder()
				req := sharedRequest(ctx, http.MethodGet, consentURL.String(), f.issuer.ID.String(), nil)
				req.Header.Set("Origin", "https://client.example")
				handler.ServeHTTP(page, req)
				require.Equal(t, http.StatusOK, page.Code, page.Body.String())
				require.Equal(t, "text/html; charset=utf-8", page.Header().Get("Content-Type"))
				require.Equal(t, "frame-ancestors 'none'", page.Header().Get("Content-Security-Policy"))
				require.Equal(t, "DENY", page.Header().Get("X-Frame-Options"))
				require.Equal(t, "no-store", page.Header().Get("Cache-Control"))
				require.Empty(t, page.Header().Get("Access-Control-Allow-Origin"))
				require.Empty(t, page.Header().Get("Access-Control-Allow-Credentials"))

				form := url.Values{oauthwire.ParamState: {stateID}, "csrf_token": {state.CSRFToken}, "action": {action}}
				posted := httptest.NewRecorder()
				handler.ServeHTTP(posted, sharedRequest(ctx, http.MethodPost, f.issuerURL+"/connect", f.issuer.ID.String(), strings.NewReader(form.Encode())))
				require.Equal(t, http.StatusSeeOther, posted.Code, posted.Body.String())
				location, err := url.Parse(posted.Header().Get("Location"))
				require.NoError(t, err)
				require.Equal(t, "client-state", location.Query().Get(oauthwire.ParamState))
				if action == "approve" {
					require.NotEmpty(t, location.Query().Get(oauthwire.ParamCode))
				} else {
					require.Equal(t, oautherr.CodeAccessDenied, location.Query().Get(oauthwire.ParamError))
				}
				location.RawQuery = ""
				require.Equal(t, redirectURI, location.String())
			})
		}
	}
}

func TestAuthenticationHostSharedAuthorizationServer_CORS(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), authenticationHostSharedIssuerURL(f.issuer.ID.String()))
	handler := middleware.CORSMiddleware("prod", ti.serverURL.String(), nil, nil)(harness.handler)
	base := authserver.SharedPath(f.issuer.ID)

	for _, tc := range []struct {
		path   string
		method string
		status int
		origin string
	}{
		{"/.well-known/oauth-authorization-server" + base, http.MethodGet, http.StatusOK, "*"},
		{base + "/.well-known/oauth-authorization-server", http.MethodGet, http.StatusOK, "*"},
		{base + "/token", http.MethodPost, http.StatusBadRequest, "*"},
		{base + "/revoke", http.MethodPost, http.StatusBadRequest, "*"},
		{base + "/register", http.MethodPost, http.StatusBadRequest, "*"},
		{base + "/authorize", http.MethodGet, http.StatusBadRequest, ""},
		{base + "/connect", http.MethodGet, http.StatusBadRequest, ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			for _, method := range []string{http.MethodOptions, tc.method} {
				req := httptest.NewRequestWithContext(ctx, method, testAuthenticationHostURL+tc.path, nil)
				req.Header.Set("Origin", "https://client.example")
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Access-Control-Request-Method", tc.method)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)

				require.Equal(t, tc.origin, w.Header().Get("Access-Control-Allow-Origin"))
				require.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
				if method == http.MethodOptions {
					require.Equal(t, http.StatusNoContent, w.Code)
				} else {
					require.Equal(t, tc.status, w.Code, w.Body.String())
				}
			}
		})
	}
}
