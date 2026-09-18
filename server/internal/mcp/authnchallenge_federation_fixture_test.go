package mcp_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/conv"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

// A code permanently binds the claims and client credentials for one exchange.
// There is deliberately no provider-wide current nonce or current secret.
type federationToken struct {
	nonce, email, issuer, secret string
	verified                     bool
}

type federationTokenRequest struct {
	code, verifier, clientID, secret string
}

type federationProvider struct {
	*httptest.Server
	signer   jose.Signer
	jwks     jose.JSONWebKeySet
	mu       sync.Mutex
	codes    map[string]federationToken
	issued   map[string]bool
	requests []federationTokenRequest
	errors   []error
}

func newFederationProvider(t *testing.T) *federationProvider {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "federation-test"))
	require.NoError(t, err)
	p := &federationProvider{signer: signer, jwks: jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: key.Public(), KeyID: "federation-test", Algorithm: "ES256", Use: "sig"}}}, codes: make(map[string]federationToken), issued: make(map[string]bool)}
	// Publish all handler state before starting the server. Derive the immutable
	// issuer from the already allocated listener, not a closure assigned after start.
	server := httptest.NewUnstartedServer(nil)
	issuer := "https://" + server.Listener.Addr().String()
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { p.serveHTTP(issuer, w, r) })
	p.Server = server
	server.StartTLS()
	t.Cleanup(func() {
		server.Close() // drain handlers before inspecting their observations
		p.mu.Lock()
		defer p.mu.Unlock()
		require.Empty(t, p.errors, "mock provider protocol errors")
		for _, request := range p.requests {
			require.NotEmpty(t, request.code)
			require.NotEmpty(t, request.verifier, "upstream PKCE is required")
			require.Equal(t, "selected-client", request.clientID)
		}
	})
	return p
}

func (p *federationProvider) issueCode(t *testing.T, code string, token federationToken) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.False(t, p.issued[code], "authorization codes must never be reused")
	p.issued[code] = true
	p.codes[code] = token
}

func (p *federationProvider) exchangeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func (p *federationProvider) serveHTTP(issuer string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var response any
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		response = map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"ES256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic"}, "code_challenge_methods_supported": []string{"S256"}, "authorization_response_iss_parameter_supported": true}
	case "/jwks":
		response = p.jwks
	case "/token":
		p.mu.Lock()
		defer p.mu.Unlock()
		id, secret, basic := r.BasicAuth()
		parseErr := r.ParseForm()
		request := federationTokenRequest{code: r.Form.Get("code"), verifier: r.Form.Get("code_verifier"), clientID: id, secret: secret}
		p.requests = append(p.requests, request)
		token, exists := p.codes[request.code]
		delete(p.codes, request.code) // consume once, including rejected exchanges
		if parseErr != nil || !exists || !basic || id != "selected-client" || secret != token.secret || request.verifier == "" {
			p.errors = append(p.errors, fmt.Errorf("invalid token exchange (parsed=%t, known code=%t, basic=%t, client=%t, secret=%t, PKCE=%t)", parseErr == nil, exists, basic, id == "selected-client", secret == token.secret, request.verifier != ""))
			http.Error(w, "invalid token exchange", http.StatusBadRequest)
			return
		}
		raw, err := jwt.Signed(p.signer).Claims(jwt.Claims{Issuer: token.issuer, Subject: "upstream-human", Audience: jwt.Audience{"selected-client"}, IssuedAt: jwt.NewNumericDate(time.Now()), Expiry: jwt.NewNumericDate(time.Now().Add(time.Minute))}).Claims(map[string]any{"nonce": token.nonce, "email": token.email, "email_verified": token.verified}).Serialize()
		if err != nil {
			p.errors = append(p.errors, fmt.Errorf("sign token: %w", err))
			http.Error(w, "sign token failed", http.StatusInternalServerError)
			return
		}
		// Deliberately no refresh_token: login is not a retention workflow.
		response = map[string]any{"access_token": "ephemeral-access-token", "token_type": "Bearer", "expires_in": 60, "id_token": raw}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			p.errors = append(p.errors, err)
		}
		return
	default:
		http.NotFound(w, r)
		return
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.errors = append(p.errors, err)
	}
}

func TestFederatedLoginCredentialRotation(t *testing.T) {
	t.Parallel()
	runFederatedLogin(t, "success", func(ctx context.Context, ti *testInstance, provider *federationProvider, clientID uuid.UUID, organizationID string, authorize, oldCallback *http.Request) (*http.Request, string, string) {
		// Rotate only after the first browser flow has bound the old configuration.
		rotated, err := ti.enc.Encrypt([]byte("rotated-secret"))
		require.NoError(t, err)
		err = remotesessionsrepo.New(ti.conn).SetOrganizationRemoteSessionClientCredentialsFixture(ctx, remotesessionsrepo.SetOrganizationRemoteSessionClientCredentialsFixtureParams{ID: clientID, OrganizationID: conv.ToPGText(organizationID), ClientID: pgtype.Text{String: "", Valid: false}, ClientSecretEncrypted: conv.ToPGText(rotated)})
		require.NoError(t, err)
		rejected := httptest.NewRecorder()
		require.NoError(t, ti.service.HandleIDPCallback(rejected, oldCallback))
		failure := assertFederationErrorRedirect(t, rejected)
		require.Equal(t, "server_error", failure.Query().Get("error"))
		require.Zero(t, provider.exchangeCount())
		require.Error(t, ti.service.HandleIDPCallback(httptest.NewRecorder(), oldCallback), "rejected old state is single-use")
		require.Zero(t, provider.exchangeCount())

		// A fresh flow binds both the new configuration and its own nonce/code.
		start := httptest.NewRecorder()
		require.NoError(t, ti.service.HandleAuthorize(start, authorize))
		begin := httptest.NewRecorder()
		require.NoError(t, ti.service.HandleIDPCallback(begin, httptest.NewRequest(http.MethodGet, start.Header().Get("Location"), nil).WithContext(ctx)))
		upstream, err := url.Parse(begin.Header().Get("Location"))
		require.NoError(t, err)
		id, nonce := upstream.Query().Get("state"), upstream.Query().Get("nonce")
		require.NotEmpty(t, id)
		require.NotEmpty(t, nonce)
		require.NotEqual(t, oldCallback.URL.Query().Get("state"), id)
		provider.issueCode(t, "rotated-one-use-code", federationToken{nonce: nonce, email: mockidp.MockUserEmail, issuer: provider.URL, verified: true, secret: "rotated-secret"})
		query := url.Values{"state": {id}, "code": {"rotated-one-use-code"}, "iss": {provider.URL}}
		callback := httptest.NewRequest(http.MethodGet, ti.serverURL.String()+"/mcp/idp_callback?"+query.Encode(), nil).WithContext(ctx)
		require.Len(t, begin.Result().Cookies(), 1)
		callback.AddCookie(begin.Result().Cookies()[0])
		// Shared completion assertions prove success, no retention, and no replay.
		return callback, nonce, id
	})
}

// A terminal federated failure is an OAuth error, not a consent redirect or an
// unhandled service error. Native clients retain their registered HTTP callback.
func assertFederationErrorRedirect(t *testing.T, result *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	require.Equal(t, http.StatusFound, result.Code)
	target, err := url.Parse(result.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "http", target.Scheme)
	require.Equal(t, "127.0.0.1", target.Host)
	require.Equal(t, "/callback", target.Path)
	require.Equal(t, "downstream-state", target.Query().Get("state"))
	require.Contains(t, []string{"access_denied", "server_error", "temporarily_unavailable"}, target.Query().Get("error"))
	require.NotEmpty(t, target.Query().Get("error_description"))
	require.Empty(t, target.Query().Get("code"))
	require.Equal(t, "no-store", result.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", result.Header().Get("Referrer-Policy"))
	return target
}
