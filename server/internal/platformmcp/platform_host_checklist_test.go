package platformmcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	// checklistCanonicalHost is the server URL's host.
	checklistCanonicalHost = "gram.example.test"

	// checklistExtraHost is the extra platform host (--platform-hosts).
	checklistExtraHost = "ai.example.test"

	// checklistEnvironment is any environment other than "local": local
	// development stamps the server URL on every request and skips host
	// classification, which is exactly what the checklist must exercise.
	checklistEnvironment = "prod"

	// checklistRedirectURI is the MCP client's loopback callback.
	checklistRedirectURI = "http://127.0.0.1:3000/callback"
)

// checklistHost is one first-party host an MCP client can be pointed at.
type checklistHost struct {
	// host is the Host header the client sends.
	host string

	// baseURL is the externally visible origin the server must render for it.
	baseURL string
}

// TestPlatformHostVerificationChecklistPlatformMCP runs the "Deprecate
// app.getgram.ai" verification checklist for the Platform MCP from an MCP
// client's point of view, once per platform host, through the real host
// classification middleware: discovery metadata and the WWW-Authenticate
// challenge name the requested host, and a full registration, authorization,
// organization selection, consent, and token flow issues tokens for that host
// that the runtime accepts there and refuses on the other host, before and
// after refresh.
func TestPlatformHostVerificationChecklistPlatformMCP(t *testing.T) {
	t.Parallel()

	for _, requested := range []string{"canonical", "extra"} {
		t.Run(requested, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_host_checklist")
			require.NoError(t, err)
			organizationID := "org_" + uuid.NewString()
			_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
				ID:          organizationID,
				Name:        "Platform host checklist organization",
				Slug:        "org-" + uuid.NewString()[:8],
				WorkosID:    pgtype.Text{},
				Whitelisted: pgtype.Bool{},
			})
			require.NoError(t, err)

			serverURL := &url.URL{Scheme: "https", Host: checklistCanonicalHost}
			platformHosts, err := customdomains.ParsePlatformHosts([]string{checklistExtraHost})
			require.NoError(t, err)
			canonical := checklistHost{host: checklistCanonicalHost, baseURL: serverURL.String()}
			extra := checklistHost{host: checklistExtraHost, baseURL: platformHosts[checklistExtraHost]}
			on, other := canonical, extra
			if requested == "extra" {
				on, other = extra, canonical
			}

			encryptionClient := testEncryption(t)
			signer := sessiontokens.NewSigner("platform-host-checklist-key")
			oauth, err := NewOAuthHTTP(OAuthHTTPConfig{
				BaseURL:       serverURL,
				Cache:         &memoryCache{values: map[string]any{}},
				Store:         NewPostgresOAuthStore(conn),
				Identity:      testIdentity{},
				Gate:          allowGate{},
				Authorizer:    allowAuthorizer{},
				Organizations: testOrganizationSelector{organizations: []OrganizationOption{{ID: organizationID, Name: "Platform host checklist organization"}}},
				Signer:        signer,
				Encryption:    encryptionClient,
			})
			require.NoError(t, err)
			authenticator, err := NewJWTAuthenticator(signer, conn, encryptionClient, serverURL)
			require.NoError(t, err)
			runtime := NewRuntime(testenv.NewLogger(t), authenticator, allowGate{}, allowAuthorizer{}, oauth.ProtectedResourceURL(), "platform-host-checklist-cursor", nil, nil, nil, nil, nil)

			logger := testenv.NewLogger(t)
			mcpSecurity, err := middleware.MCPSecurity(logger, append([]string{serverURL.String()}, slices.Sorted(maps.Values(platformHosts))...))
			require.NoError(t, err)
			mux := goahttp.NewMuxer()
			mux.Use(mcpSecurity)
			mux.Use(customdomains.Middleware(logger, conn, checklistEnvironment, serverURL, platformHosts))
			mux.Use(middleware.SessionMiddleware)
			oauth.Attach(mux)
			mux.Handle(http.MethodPost, Path, runtime.Handler().ServeHTTP)

			serve := func(method, host, target string, body []byte, header http.Header) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
				req.Host = host
				maps.Copy(req.Header, header)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)
				return w
			}
			formHeader := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
			runtimeHeader := func(accessToken string) http.Header {
				header := http.Header{"Accept": {"application/json, text/event-stream"}, "Content-Type": {"application/json"}}
				if accessToken != "" {
					header.Set("Authorization", "Bearer "+accessToken)
				}
				return header
			}
			initialize := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"platform-host-checklist","version":"1.0.0"}}}`)
			resource := on.baseURL + Path

			// 1. Discovery names the requested host.
			protected := serve(http.MethodGet, on.host, "/.well-known/oauth-protected-resource/platform-mcp", nil, nil)
			require.Equal(t, http.StatusOK, protected.Code, protected.Body.String())
			var protectedMeta struct {
				Resource             string   `json:"resource"`
				AuthorizationServers []string `json:"authorization_servers"`
			}
			require.NoError(t, json.Unmarshal(protected.Body.Bytes(), &protectedMeta))
			require.Equal(t, resource, protectedMeta.Resource)
			require.Equal(t, []string{resource}, protectedMeta.AuthorizationServers)

			server := serve(http.MethodGet, on.host, "/.well-known/oauth-authorization-server/platform-mcp", nil, nil)
			require.Equal(t, http.StatusOK, server.Code, server.Body.String())
			var serverMeta map[string]any
			require.NoError(t, json.Unmarshal(server.Body.Bytes(), &serverMeta))
			require.Equal(t, resource, serverMeta["issuer"])
			require.Equal(t, resource+"/authorize", serverMeta["authorization_endpoint"])
			require.Equal(t, resource+"/token", serverMeta["token_endpoint"])
			require.Equal(t, resource+"/register", serverMeta["registration_endpoint"])

			challenge := serve(http.MethodPost, on.host, Path, initialize, runtimeHeader(""))
			require.Equal(t, http.StatusUnauthorized, challenge.Code, challenge.Body.String())
			require.Equal(t, `Bearer resource_metadata="`+on.baseURL+`/.well-known/oauth-protected-resource/platform-mcp"`, challenge.Header().Get("WWW-Authenticate"))

			// 2. Registration, authorization, organization selection, consent,
			// and token exchange.
			register := serve(http.MethodPost, on.host, "/platform-mcp/register", []byte(`{"client_name":"platform host checklist","redirect_uris":["`+checklistRedirectURI+`"],"token_endpoint_auth_method":"none"}`), http.Header{"Content-Type": {"application/json"}})
			require.Equal(t, http.StatusCreated, register.Code, register.Body.String())
			var registered struct {
				ClientID string `json:"client_id"`
			}
			require.NoError(t, json.Unmarshal(register.Body.Bytes(), &registered))
			require.NotEmpty(t, registered.ClientID)

			verifier := strings.Repeat("v", 43)
			digest := sha256.Sum256([]byte(verifier))
			authorizeQuery := url.Values{
				"response_type":         {"code"},
				"client_id":             {registered.ClientID},
				"redirect_uri":          {checklistRedirectURI},
				"code_challenge":        {base64.RawURLEncoding.EncodeToString(digest[:])},
				"code_challenge_method": {"S256"},
				"resource":              {resource},
			}
			authorize := serve(http.MethodGet, on.host, "/platform-mcp/authorize?"+authorizeQuery.Encode(), nil, nil)
			require.Equal(t, http.StatusFound, authorize.Code, authorize.Body.String())
			idpURL, err := url.Parse(authorize.Header().Get("Location"))
			require.NoError(t, err)

			// The identity provider only knows the server URL's callback.
			callback := serve(http.MethodGet, canonical.host, "/platform-mcp/idp_callback?"+url.Values{"state": {idpURL.Query().Get("state")}, "code": {"idp-code"}}.Encode(), nil, nil)
			require.Equal(t, http.StatusFound, callback.Code, callback.Body.String())
			selectionURL, err := url.Parse(callback.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, on.baseURL+"/platform-mcp/select-organization", selectionURL.Scheme+"://"+selectionURL.Host+selectionURL.Path, "the browser must return to the requested host")

			selection := serve(http.MethodGet, on.host, selectionURL.RequestURI(), nil, nil)
			require.Equal(t, http.StatusOK, selection.Code, selection.Body.String())
			state := selectionURL.Query().Get("state")
			_, afterCSRF, found := strings.Cut(selection.Body.String(), `name="csrf_token" value="`)
			require.True(t, found)
			csrf, _, _ := strings.Cut(afterCSRF, `"`)

			selected := serve(http.MethodPost, on.host, "/platform-mcp/select-organization", []byte(url.Values{"state": {state}, "csrf_token": {csrf}, "organization_id": {organizationID}}.Encode()), formHeader)
			require.Equal(t, http.StatusSeeOther, selected.Code, selected.Body.String())
			connectURL, err := url.Parse(selected.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, on.baseURL+"/platform-mcp/connect", connectURL.Scheme+"://"+connectURL.Host+connectURL.Path)

			approve := serve(http.MethodPost, on.host, "/platform-mcp/connect", []byte(url.Values{"state": {state}, "csrf_token": {csrf}, "action": {"approve"}}.Encode()), formHeader)
			require.Equal(t, http.StatusSeeOther, approve.Code, approve.Body.String())
			clientRedirect, err := url.Parse(approve.Header().Get("Location"))
			require.NoError(t, err)
			code := clientRedirect.Query().Get("code")
			require.NotEmpty(t, code)

			codeForm := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {checklistRedirectURI}, "code_verifier": {verifier}, "client_id": {registered.ClientID}}
			wrongHostCode := serve(http.MethodPost, other.host, "/platform-mcp/token", []byte(codeForm.Encode()), formHeader)
			require.Equal(t, http.StatusBadRequest, wrongHostCode.Code, wrongHostCode.Body.String())
			require.Contains(t, wrongHostCode.Body.String(), `"invalid_grant"`)

			exchanged := serve(http.MethodPost, on.host, "/platform-mcp/token", []byte(codeForm.Encode()), formHeader)
			require.Equal(t, http.StatusOK, exchanged.Code, exchanged.Body.String())
			var tokens struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			}
			require.NoError(t, json.Unmarshal(exchanged.Body.Bytes(), &tokens))
			claims, err := signer.ValidateExactAudience(tokens.AccessToken, resource)
			require.NoError(t, err)
			require.Equal(t, resource, claims.Issuer)

			accepted := serve(http.MethodPost, on.host, Path, initialize, runtimeHeader(tokens.AccessToken))
			require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
			refused := serve(http.MethodPost, other.host, Path, initialize, runtimeHeader(tokens.AccessToken))
			require.Equal(t, http.StatusUnauthorized, refused.Code, "a token minted on %s must be refused on %s: %s", on.host, other.host, refused.Body.String())
			require.Equal(t, `Bearer resource_metadata="`+other.baseURL+`/.well-known/oauth-protected-resource/platform-mcp"`, refused.Header().Get("WWW-Authenticate"))

			refreshForm := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens.RefreshToken}, "client_id": {registered.ClientID}}
			wrongHostRefresh := serve(http.MethodPost, other.host, "/platform-mcp/token", []byte(refreshForm.Encode()), formHeader)
			require.Equal(t, http.StatusBadRequest, wrongHostRefresh.Code, wrongHostRefresh.Body.String())
			require.Contains(t, wrongHostRefresh.Body.String(), `"invalid_grant"`)

			refreshed := serve(http.MethodPost, on.host, "/platform-mcp/token", []byte(refreshForm.Encode()), formHeader)
			require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
			var refreshedTokens struct {
				AccessToken string `json:"access_token"`
			}
			require.NoError(t, json.Unmarshal(refreshed.Body.Bytes(), &refreshedTokens))
			refreshedAccepted := serve(http.MethodPost, on.host, Path, initialize, runtimeHeader(refreshedTokens.AccessToken))
			require.Equal(t, http.StatusOK, refreshedAccepted.Code, refreshedAccepted.Body.String())
			refreshedRefused := serve(http.MethodPost, other.host, Path, initialize, runtimeHeader(refreshedTokens.AccessToken))
			require.Equal(t, http.StatusUnauthorized, refreshedRefused.Code, refreshedRefused.Body.String())
		})
	}
}
