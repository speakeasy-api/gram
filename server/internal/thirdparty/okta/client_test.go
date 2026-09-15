package okta_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

func TestAcquireTokenSignsPrivateKeyJWTAndCachesByConnectionAndKey(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	connectionID := uuid.New()
	validation := make(chan error, 1)
	var requests atomic.Int64
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if err := r.ParseForm(); err != nil {
			validation <- err
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/oauth2/v1/token" {
			validation <- fmt.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("scope") != "scope.one scope.two" || r.Form.Get("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
			validation <- errors.New("unexpected token form")
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		token, err := jwt.ParseSigned(r.Form.Get("client_assertion"), []jose.SignatureAlgorithm{jose.RS256})
		if err != nil {
			validation <- err
			http.Error(w, "invalid assertion", http.StatusBadRequest)
			return
		}
		var claims jwt.Claims
		if err := token.Claims(&privateKey.PublicKey, &claims); err != nil {
			validation <- err
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		if len(token.Headers) != 1 || token.Headers[0].KeyID != "test-kid" || claims.Issuer != "test-client-id" || claims.Subject != "test-client-id" || len(claims.Audience) != 1 || claims.Audience[0] != endpoint+"/oauth2/v1/token" || claims.ID == "" || claims.Expiry == nil || claims.IssuedAt == nil || claims.Expiry.Time().Sub(claims.IssuedAt.Time()) != 5*time.Minute {
			validation <- errors.New("unexpected assertion claims")
			http.Error(w, "invalid claims", http.StatusUnauthorized)
			return
		}
		validation <- nil
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-access-token","expires_in":3600,"scope":"scope.one scope.two"}`))
	}))
	t.Cleanup(server.Close)
	endpoint = server.URL
	client := newTestClient(t, endpoint)

	request := okta.TokenRequest{
		ConnectionID: connectionID,
		TenantDomain: "example.okta.com",
		ClientID:     "test-client-id",
		KeyID:        "test-kid",
		PrivateKey:   privateKey,
		Scopes:       []string{"scope.one", "scope.two"},
	}
	first, err := client.AcquireToken(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, <-validation)
	second, err := client.AcquireToken(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, int64(1), requests.Load())
}

func TestAcquireTokenAdaptsToDPoPAndRetriesTokenAndResourceNonces(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var tokenRequests atomic.Int64
	var resourceRequests atomic.Int64
	var validationMu sync.Mutex
	var validationErr error
	var dpopPublicKey *ecdsa.PublicKey
	clientAssertionIDs := make(map[string]struct{})
	var endpoint string
	recordValidationError := func(err error) {
		if err == nil {
			return
		}
		validationMu.Lock()
		defer validationMu.Unlock()
		if validationErr == nil {
			validationErr = err
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth2/v1/token":
			if err := r.ParseForm(); err != nil {
				recordValidationError(fmt.Errorf("parse token form: %w", err))
			}
			assertionID, err := validateClientAssertionID(r.Form.Get("client_assertion"), &privateKey.PublicKey)
			recordValidationError(err)
			if _, duplicate := clientAssertionIDs[assertionID]; duplicate {
				recordValidationError(fmt.Errorf("reused client assertion jti %q", assertionID))
			}
			clientAssertionIDs[assertionID] = struct{}{}
			requestNumber := tokenRequests.Add(1)
			switch requestNumber {
			case 1:
				if r.Header.Get("DPoP") != "" {
					recordValidationError(errors.New("initial token request unexpectedly used DPoP"))
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_dpop_proof","error_description":"The DPoP proof JWT header is missing"}`))
			case 2:
				key, err := validateDPoPProof(r.Header.Get("DPoP"), http.MethodPost, endpoint+"/oauth2/v1/token", "", "")
				recordValidationError(err)
				dpopPublicKey = key
				w.Header().Set("DPoP-Nonce", "token-nonce")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"use_dpop_nonce","error_description":"Retry with the supplied nonce"}`))
			case 3:
				key, err := validateDPoPProof(r.Header.Get("DPoP"), http.MethodPost, endpoint+"/oauth2/v1/token", "", "token-nonce")
				recordValidationError(err)
				recordValidationError(requireSameDPoPKey(dpopPublicKey, key))
				_, _ = w.Write([]byte(`{"access_token":"dpop-access-token","token_type":"DPoP","expires_in":3600,"scope":"scope.one"}`))
			case 4:
				key, err := validateDPoPProof(r.Header.Get("DPoP"), http.MethodPost, endpoint+"/oauth2/v1/token", "", "token-nonce")
				recordValidationError(err)
				recordValidationError(requireSameDPoPKey(dpopPublicKey, key))
				_, _ = w.Write([]byte(`{"access_token":"replacement-dpop-access-token","token_type":"DPoP","expires_in":3600,"scope":"scope.one"}`))
			default:
				recordValidationError(fmt.Errorf("unexpected token request %d", requestNumber))
				http.Error(w, "unexpected token request", http.StatusBadRequest)
			}
		case "/api/v1/groups":
			requestNumber := resourceRequests.Add(1)
			if r.Header.Get("Authorization") != "DPoP dpop-access-token" {
				recordValidationError(fmt.Errorf("unexpected authorization %q", r.Header.Get("Authorization")))
			}
			nonce := ""
			if requestNumber >= 2 {
				nonce = "resource-nonce"
			}
			key, err := validateDPoPProof(r.Header.Get("DPoP"), http.MethodGet, endpoint+"/api/v1/groups", "dpop-access-token", nonce)
			recordValidationError(err)
			recordValidationError(requireSameDPoPKey(dpopPublicKey, key))
			if requestNumber == 1 {
				w.Header().Set("DPoP-Nonce", "resource-nonce")
				w.Header().Set("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error_description":"Retry with the supplied nonce"}`))
				return
			}
			if requestNumber > 3 {
				recordValidationError(fmt.Errorf("unexpected resource request %d", requestNumber))
				http.Error(w, "unexpected resource request", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`[{"id":"group-1"}]`))
		default:
			recordValidationError(fmt.Errorf("unexpected path %s", r.URL.Path))
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	endpoint = server.URL
	client := newTestClient(t, endpoint)

	tokenRequest := okta.TokenRequest{
		ConnectionID: uuid.New(),
		TenantDomain: "example.okta.com",
		ClientID:     "test-client-id",
		KeyID:        "test-kid",
		PrivateKey:   privateKey,
		Scopes:       []string{"scope.one"},
	}
	token, err := client.AcquireToken(t.Context(), tokenRequest)
	require.NoError(t, err)
	require.Equal(t, "dpop-access-token", token.AccessToken)
	page, err := client.ListGroups(t.Context(), "example.okta.com", token.AccessToken, okta.PageRequest{Limit: 1, After: "cursor-is-not-in-htu"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	tokenRequest.ClientID = "replacement-client-id"
	replacement, err := client.AcquireToken(t.Context(), tokenRequest)
	require.NoError(t, err)
	require.Equal(t, "replacement-dpop-access-token", replacement.AccessToken)
	page, err = client.ListGroups(t.Context(), "example.okta.com", token.AccessToken, okta.PageRequest{Limit: 1, After: ""})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, int64(4), tokenRequests.Load())
	require.Equal(t, int64(3), resourceRequests.Load())
	validationMu.Lock()
	require.NoError(t, validationErr)
	validationMu.Unlock()
}

func TestAcquireTokenKeepsBearerForNonDPoPTenant(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var endpoint string
	var validationMu sync.Mutex
	var validationErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth2/v1/token":
			if r.Header.Get("DPoP") != "" {
				validationMu.Lock()
				validationErr = errors.New("non-DPoP token request included a proof")
				validationMu.Unlock()
			}
			_, _ = w.Write([]byte(`{"access_token":"bearer-access-token","token_type":"Bearer","expires_in":3600,"scope":"scope.one"}`))
		case "/api/v1/groups":
			if r.Header.Get("Authorization") != "Bearer bearer-access-token" || r.Header.Get("DPoP") != "" {
				validationMu.Lock()
				validationErr = fmt.Errorf("unexpected bearer request headers: authorization=%q dpop=%q", r.Header.Get("Authorization"), r.Header.Get("DPoP"))
				validationMu.Unlock()
			}
			_, _ = w.Write([]byte(`[{"id":"group-1"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	endpoint = server.URL
	client := newTestClient(t, endpoint)
	token, err := client.AcquireToken(t.Context(), okta.TokenRequest{
		ConnectionID: uuid.New(),
		TenantDomain: "example.okta.com",
		ClientID:     "test-client-id",
		KeyID:        "test-kid",
		PrivateKey:   privateKey,
		Scopes:       []string{"scope.one"},
	})
	require.NoError(t, err)
	_, err = client.ListGroups(t.Context(), "example.okta.com", token.AccessToken, okta.PageRequest{Limit: 1, After: ""})
	require.NoError(t, err)
	validationMu.Lock()
	require.NoError(t, validationErr)
	validationMu.Unlock()
}

func TestListGroupsSurfacesCursorAndRateLimit(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(time.Minute).Unix()
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access-token" || r.URL.Query().Get("limit") != "1" || r.URL.Query().Get("after") != "current-cursor" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Link", `<`+endpoint+`/api/v1/groups?after=next-cursor&limit=1>; rel="next"`)
		w.Header().Set("X-Rate-Limit-Limit", "100")
		w.Header().Set("X-Rate-Limit-Remaining", "42")
		w.Header().Set("X-Rate-Limit-Reset", fmt.Sprintf("%d", resetAt))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"group-1"}]`))
	}))
	t.Cleanup(server.Close)
	endpoint = server.URL

	page, err := newTestClient(t, endpoint).ListGroups(t.Context(), "example.okta.com", "test-access-token", okta.PageRequest{Limit: 1, After: "current-cursor"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "next-cursor", page.NextCursor)
	require.NotNil(t, page.RateLimit.Limit)
	require.Equal(t, int64(100), *page.RateLimit.Limit)
	require.NotNil(t, page.RateLimit.Remaining)
	require.Equal(t, int64(42), *page.RateLimit.Remaining)
	require.NotNil(t, page.RateLimit.Reset)
	require.Equal(t, resetAt, page.RateLimit.Reset.Unix())
}

func TestAcquireTokenReturnsTypedOktaError(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	assertions := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		assertions <- r.Form.Get("client_assertion")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client", "error_description": "Rejected assertion " + r.Form.Get("client_assertion")})
	}))
	t.Cleanup(server.Close)

	_, err = newTestClientWithLogger(t, server.URL, logger).AcquireToken(t.Context(), okta.TokenRequest{
		ConnectionID: uuid.New(),
		TenantDomain: "example.okta.com",
		ClientID:     "test-client-id",
		KeyID:        "test-kid",
		PrivateKey:   privateKey,
		Scopes:       []string{"scope.one"},
	})
	var apiErr *okta.APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	require.Equal(t, "invalid_client", apiErr.Code)
	require.Equal(t, "Rejected assertion [redacted]", apiErr.Description)
	require.NotContains(t, apiErr.Error(), "test-access-token")
	rawAssertion := <-assertions
	require.Contains(t, logs.String(), "Okta token request failed")
	require.Contains(t, logs.String(), "invalid_client")
	require.Contains(t, logs.String(), "Rejected assertion [redacted]")
	require.Contains(t, logs.String(), "example.okta.com")
	require.Contains(t, logs.String(), "test-client-id")
	require.Contains(t, logs.String(), "test-kid")
	require.Contains(t, logs.String(), "RS256")
	require.Contains(t, logs.String(), "gram.oauth.assertion_audience")
	require.Contains(t, logs.String(), "gram.oauth.assertion_expires_at")
	require.Contains(t, logs.String(), server.URL+"/oauth2/v1/token")
	require.NotContains(t, logs.String(), rawAssertion)
}

func TestAcquireTokenDoesNotReuseTokenAfterClientIDChanges(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestNumber := requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":3600,"scope":"scope.one"}`, requestNumber)
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server.URL)
	connectionID := uuid.New()
	request := okta.TokenRequest{ConnectionID: connectionID, TenantDomain: "example.okta.com", ClientID: "client-one", KeyID: "test-kid", PrivateKey: privateKey, Scopes: []string{"scope.one"}}
	first, err := client.AcquireToken(t.Context(), request)
	require.NoError(t, err)
	request.ClientID = "client-two"
	second, err := client.AcquireToken(t.Context(), request)
	require.NoError(t, err)
	require.NotEqual(t, first.AccessToken, second.AccessToken)
	require.Equal(t, int64(2), requests.Load())
}

func TestListGroupsRedactsReflectedAccessToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"errorCode": "E0000006", "errorSummary": "Rejected " + r.Header.Get("Authorization")})
	}))
	t.Cleanup(server.Close)

	_, err := newTestClient(t, server.URL).ListGroups(t.Context(), "example.okta.com", "reflected-access-token", okta.PageRequest{Limit: 1, After: ""})
	var apiErr *okta.APIError
	require.ErrorAs(t, err, &apiErr)
	require.NotContains(t, apiErr.Description, "reflected-access-token")
	require.Contains(t, apiErr.Description, "[redacted]")
}

func TestAcquireTokenDoesNotRetryClientAssertion(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server_error","error_description":"Temporary failure."}`))
	}))
	t.Cleanup(server.Close)

	_, err = newTestClientWithRetries(t, server.URL, 2).AcquireToken(t.Context(), okta.TokenRequest{
		ConnectionID: uuid.New(),
		TenantDomain: "example.okta.com",
		ClientID:     "test-client-id",
		KeyID:        "test-kid",
		PrivateKey:   privateKey,
		Scopes:       []string{"scope.one"},
	})
	var apiErr *okta.APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	require.Equal(t, int64(1), requests.Load())
}

func TestListGroupsBoundsSuccessfulResponseBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"value":"` + strings.Repeat("x", 2*1024*1024) + `"}]`))
	}))
	t.Cleanup(server.Close)

	_, err := newTestClient(t, server.URL).ListGroups(t.Context(), "example.okta.com", "test-access-token", okta.PageRequest{Limit: 1, After: ""})
	require.Error(t, err)
	require.ErrorContains(t, err, "decode Okta response")
}

func TestCreateOIDCApplicationSendsExactPayloadAndDropsResponseSecret(t *testing.T) {
	t.Parallel()

	const responseSecret = "response-secret-must-not-escape"
	requests := make(chan *http.Request, 1)
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		requests <- r.Clone(t.Context())
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"app-1","status":"ACTIVE","label":"Speakeasy","credentials":{"oauthClient":{"client_id":"client-1","client_secret":"` + responseSecret + `"}}}`))
	}))
	t.Cleanup(server.Close)

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	app, err := newTestClientWithLogger(t, server.URL, logger).CreateOIDCApplication(t.Context(), "example.okta.com", "test-access-token", okta.CreateOIDCApplicationInput{
		RedirectURIs: []string{"https://auth.example.com/sso/callback", "https://app.example.com/oauth/callback"},
		ClientSecret: "caller-minted-secret",
	})
	require.NoError(t, err)
	require.Equal(t, okta.Application{ID: "app-1", Status: "ACTIVE", Label: "Speakeasy", ClientID: "client-1"}, app)

	request := <-requests
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "/api/v1/apps", request.URL.Path)
	require.Equal(t, "Bearer test-access-token", request.Header.Get("Authorization"))
	require.Equal(t, "application/json", request.Header.Get("Content-Type"))
	body := <-bodies
	require.JSONEq(t, `{
		"name":"oidc_client",
		"label":"Speakeasy",
		"signOnMode":"OPENID_CONNECT",
		"credentials":{"oauthClient":{"client_secret":"caller-minted-secret","token_endpoint_auth_method":"client_secret_post","autoKeyRotation":true,"pkce_required":true}},
		"settings":{"oauthClient":{"application_type":"web","grant_types":["authorization_code","refresh_token"],"response_types":["code"],"redirect_uris":["https://auth.example.com/sso/callback","https://app.example.com/oauth/callback"],"consent_method":"TRUSTED"}}
	}`, string(body))
	require.Contains(t, string(body), `"client_secret":"caller-minted-secret"`)
	_, exposesSecret := reflect.TypeOf(app).FieldByName("ClientSecret")
	require.False(t, exposesSecret)
	encoded, err := json.Marshal(app)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), responseSecret)
	require.NotContains(t, fmt.Sprintf("%+v", app), responseSecret)
	require.NotContains(t, logs.String(), responseSecret)
	require.NotContains(t, logs.String(), "caller-minted-secret")
}

func TestCreateOIDCApplicationRedactsReflectedClientSecret(t *testing.T) {
	t.Parallel()

	const clientSecret = "caller-minted-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorCode":"E0000001","errorSummary":"Rejected ` + clientSecret + `"}`))
	}))
	t.Cleanup(server.Close)

	_, err := newTestClient(t, server.URL).CreateOIDCApplication(t.Context(), "example.okta.com", "test-access-token", okta.CreateOIDCApplicationInput{
		RedirectURIs: []string{"https://auth.example.com/sso/callback"},
		ClientSecret: clientSecret,
	})
	var apiErr *okta.APIError
	require.ErrorAs(t, err, &apiErr)
	require.NotContains(t, apiErr.Description, clientSecret)
	require.Contains(t, apiErr.Description, "[redacted]")
}

func TestGetApplicationReturnsOnlyNonSecretFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/apps/app-123" || r.Header.Get("Authorization") != "Bearer test-access-token" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"app-123","status":"INACTIVE","label":"Speakeasy","credentials":{"oauthClient":{"client_id":"client-123","client_secret":"discard-me"}},"settings":{"oauthClient":{"redirect_uris":["https://auth.example.com/callback"]}}}`))
	}))
	t.Cleanup(server.Close)

	app, err := newTestClient(t, server.URL).GetApplication(t.Context(), "example.okta.com", "test-access-token", "app-123")
	require.NoError(t, err)
	require.Equal(t, okta.Application{ID: "app-123", Status: "INACTIVE", Label: "Speakeasy", ClientID: "client-123"}, app)
	require.NotContains(t, fmt.Sprintf("%+v", app), "discard-me")
}

func TestResolveApplicationByClientIDUsesSafeFilterAndExactFallbackMatch(t *testing.T) {
	t.Parallel()

	const clientID = `client" or label eq "Other`
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/apps" || r.URL.Query().Get("limit") != "200" || r.Header.Get("Authorization") != "Bearer test-access-token" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if requests.Add(1) == 1 {
			if r.URL.Query().Get("filter") != `credentials.oauthClient.client_id eq "client\" or label eq \"Other"` {
				http.Error(w, "unsafe filter", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errorCode":"E0000001","errorSummary":"filter not supported"}`))
			return
		}
		if r.URL.Query().Has("filter") {
			http.Error(w, "unexpected fallback filter", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"wrong-app","status":"ACTIVE","label":"Other","credentials":{"oauthClient":{"client_id":"wrong-client"}}},
			{"id":"matching-app","status":"ACTIVE","label":"Speakeasy","credentials":{"oauthClient":{"client_id":"client\" or label eq \"Other","client_secret":"discard-me"}}}
		]`))
	}))
	t.Cleanup(server.Close)

	app, err := newTestClient(t, server.URL).ResolveApplicationByClientID(t.Context(), "example.okta.com", "test-access-token", clientID)
	require.NoError(t, err)
	require.Equal(t, okta.Application{ID: "matching-app", Status: "ACTIVE", Label: "Speakeasy", ClientID: clientID}, app)
	require.NotContains(t, fmt.Sprintf("%+v", app), "discard-me")
	require.Equal(t, int64(2), requests.Load())
}

func TestFindEveryoneGroupAndAssignItToApplication(t *testing.T) {
	t.Parallel()

	assignmentBodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			http.Error(w, "invalid authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/groups":
			if r.URL.Query().Get("filter") != `type eq "BUILT_IN"` || r.URL.Query().Get("limit") != "200" {
				http.Error(w, "invalid filter", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`[
				{"id":"custom-everyone","type":"OKTA_GROUP","profile":{"name":"Everyone"}},
				{"id":"built-in-admins","type":"BUILT_IN","profile":{"name":"Okta Administrators"}},
				{"id":"everyone-group","type":"BUILT_IN","profile":{"name":"Everyone"}}
			]`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/apps/app-123/groups/everyone-group":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			assignmentBodies <- body
			_, _ = w.Write([]byte(`{"id":"everyone-group"}`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server.URL)

	group, err := client.FindEveryoneGroup(t.Context(), "example.okta.com", "test-access-token")
	require.NoError(t, err)
	require.Equal(t, okta.Group{ID: "everyone-group", Name: "Everyone"}, group)
	require.NoError(t, client.AssignGroupToApplication(t.Context(), "example.okta.com", "test-access-token", "app-123", group.ID))
	require.JSONEq(t, `{}`, string(<-assignmentBodies))
}

func TestOIDCApplicationMutationsDoNotRetry(t *testing.T) {
	t.Parallel()

	var posts atomic.Int64
	var puts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			posts.Add(1)
		case http.MethodPut:
			puts.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errorCode":"E0000009","errorSummary":"temporary failure"}`))
	}))
	t.Cleanup(server.Close)
	client := newTestClientWithRetries(t, server.URL, 3)

	_, createErr := client.CreateOIDCApplication(t.Context(), "example.okta.com", "test-access-token", okta.CreateOIDCApplicationInput{RedirectURIs: []string{"https://auth.example.com/callback"}, ClientSecret: "caller-minted-secret"})
	require.Error(t, createErr)
	assignErr := client.AssignGroupToApplication(t.Context(), "example.okta.com", "test-access-token", "app-123", "group-123")
	require.Error(t, assignErr)
	require.Equal(t, int64(1), posts.Load())
	require.Equal(t, int64(1), puts.Load())
}

func newTestClient(t *testing.T, endpoint string) *okta.Client {
	t.Helper()
	return newTestClientWithLoggerAndRetries(t, endpoint, testenv.NewLogger(t), 0)
}

func newTestClientWithRetries(t *testing.T, endpoint string, attempts int) *okta.Client {
	t.Helper()
	return newTestClientWithLoggerAndRetries(t, endpoint, testenv.NewLogger(t), attempts)
}

func newTestClientWithLogger(t *testing.T, endpoint string, logger *slog.Logger) *okta.Client {
	t.Helper()
	return newTestClientWithLoggerAndRetries(t, endpoint, logger, 0)
}

func newTestClientWithLoggerAndRetries(t *testing.T, endpoint string, logger *slog.Logger, attempts int) *okta.Client {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	retries := guardian.DefaultRetryConfig()
	retries.MaxAttempts = attempts
	retries.ErrorHandler = retryablehttp.PassthroughErrorHandler
	return okta.NewClient(logger, policy, okta.ClientOpts{Endpoint: endpoint, RetryConfig: retries})
}

func validateDPoPProof(raw, method, targetURL, accessToken, nonce string) (*ecdsa.PublicKey, error) {
	if raw == "" {
		return nil, errors.New("DPoP proof is missing")
	}
	proof, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return nil, fmt.Errorf("parse DPoP proof: %w", err)
	}
	if len(proof.Headers) != 1 || proof.Headers[0].Algorithm != string(jose.ES256) || proof.Headers[0].ExtraHeaders[jose.HeaderKey("typ")] != "dpop+jwt" || proof.Headers[0].JSONWebKey == nil {
		return nil, errors.New("unexpected DPoP header")
	}
	publicKey, ok := proof.Headers[0].JSONWebKey.Key.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("unexpected DPoP public key type %T", proof.Headers[0].JSONWebKey.Key)
	}
	var registered jwt.Claims
	var claims struct {
		HTTPMethod      string `json:"htm"`
		HTTPURI         string `json:"htu"`
		AccessTokenHash string `json:"ath"`
		Nonce           string `json:"nonce"`
	}
	if err := proof.Claims(publicKey, &registered, &claims); err != nil {
		return nil, fmt.Errorf("verify DPoP proof: %w", err)
	}
	if registered.ID == "" || registered.IssuedAt == nil {
		return nil, errors.New("DPoP jti and iat are required")
	}
	expectedATH := ""
	if accessToken != "" {
		hash := sha256.Sum256([]byte(accessToken))
		expectedATH = base64.RawURLEncoding.EncodeToString(hash[:])
	}
	if claims.HTTPMethod != method || claims.HTTPURI != targetURL || claims.AccessTokenHash != expectedATH || claims.Nonce != nonce {
		return nil, fmt.Errorf("unexpected DPoP claims: %+v", claims)
	}
	return publicKey, nil
}

func validateClientAssertionID(raw string, publicKey *rsa.PublicKey) (string, error) {
	assertion, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return "", fmt.Errorf("parse client assertion: %w", err)
	}
	var claims jwt.Claims
	if err := assertion.Claims(publicKey, &claims); err != nil {
		return "", fmt.Errorf("verify client assertion: %w", err)
	}
	if claims.ID == "" {
		return "", errors.New("client assertion jti is missing")
	}
	return claims.ID, nil
}

func requireSameDPoPKey(expected, actual *ecdsa.PublicKey) error {
	if expected == nil || actual == nil || expected.Curve != actual.Curve || expected.X.Cmp(actual.X) != 0 || expected.Y.Cmp(actual.Y) != 0 {
		return errors.New("DPoP public key changed")
	}
	return nil
}
