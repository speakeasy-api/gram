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
		requestNumber := requests.Add(1)
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
		_, _ = fmt.Fprintf(w, `{"access_token":"test-access-token-%d","expires_in":3600,"scope":"scope.one scope.two"}`, requestNumber)
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
	fresh, err := client.AcquireFreshToken(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, <-validation)
	require.NotEqual(t, first.AccessToken, fresh.AccessToken)
	require.Equal(t, int64(2), requests.Load())
	replacement, err := client.AcquireToken(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, fresh, replacement)
	client.InvalidateToken(connectionID)
	_, err = client.AcquireToken(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, <-validation)
	require.Equal(t, int64(3), requests.Load())
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
				w.WriteHeader(http.StatusUnauthorized)
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

func TestListApplicationAssignmentsUsesApplicationEndpoints(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.RequestURI()
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			http.Error(w, "invalid authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"assignment-1"}]`))
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server.URL)

	groups, err := client.ListApplicationGroups(t.Context(), "example.okta.com", "test-access-token", "app-123", okta.PageRequest{Limit: 200, After: "group-cursor"})
	require.NoError(t, err)
	require.Len(t, groups.Items, 1)
	users, err := client.ListApplicationUsers(t.Context(), "example.okta.com", "test-access-token", "app-123", okta.PageRequest{Limit: 200, After: "user-cursor"})
	require.NoError(t, err)
	require.Len(t, users.Items, 1)
	require.Equal(t, "/api/v1/apps/app-123/groups?after=group-cursor&expand=group&limit=200", <-requests)
	require.Equal(t, "/api/v1/apps/app-123/users?after=user-cursor&limit=200", <-requests)
}

func TestDecodeApplicationPrefersFirstAppLink(t *testing.T) {
	t.Parallel()

	application, err := okta.DecodeApplication(json.RawMessage(`{
		"id":"app-123",
		"status":"ACTIVE",
		"label":"Example",
		"signOnMode":"SAML_2_0",
		"settings":{"app":{"url":"https://fallback.example.test"}},
		"_links":{
			"appLinks":[{"href":"https://launch.example.test"}],
			"logo":[
				{"name":"large","href":"https://cdn.example.test/large.png","type":"image/png"},
				{"name":"medium","href":"https://cdn.example.test/medium.png","type":"image/png"}
			]
		}
	}`))
	require.NoError(t, err)
	require.Equal(t, okta.Application{
		ID: "app-123", Status: "ACTIVE", Label: "Example", ClientID: "", SignOnMode: "SAML_2_0", SignOnURL: "https://launch.example.test", LogoURL: "https://cdn.example.test/medium.png",
	}, application)
}

func TestDecodeApplicationFallsBackToConfiguredURL(t *testing.T) {
	t.Parallel()

	application, err := okta.DecodeApplication(json.RawMessage(`{
		"id":"app-123",
		"label":"Example",
		"settings":{"app":{"url":"https://fallback.example.test"}}
	}`))
	require.NoError(t, err)
	require.Equal(t, "https://fallback.example.test", application.SignOnURL)
}

func TestDecodeApplicationUserUsesAssignmentScope(t *testing.T) {
	t.Parallel()

	user, err := okta.DecodeApplicationUser(json.RawMessage(`{"id":"user-1","scope":"USER"}`))
	require.NoError(t, err)
	require.Equal(t, okta.ApplicationUser{ID: "user-1", Direct: true}, user)
	user, err = okta.DecodeApplicationUser(json.RawMessage(`{"id":"user-2","scope":"GROUP"}`))
	require.NoError(t, err)
	require.Equal(t, okta.ApplicationUser{ID: "user-2", Direct: false}, user)
	_, err = okta.DecodeApplicationUser(json.RawMessage(`{"id":"user-3"}`))
	require.ErrorContains(t, err, "unexpected scope")
	_, err = okta.DecodeApplicationUser(json.RawMessage(`{"scope":"USER"}`))
	require.ErrorContains(t, err, "id is required")
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

func TestListAuthorizationServersAndCreateGroupsClaim(t *testing.T) {
	t.Parallel()

	claimBodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("Accept") != "application/json" {
			http.Error(w, "invalid authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/authorizationServers":
			_, _ = w.Write([]byte(`[{"id":"server-other","name":"other"},{"id":"server-default","name":"default"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/authorizationServers/server-default/claims":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			claimBodies <- body
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"claim-groups"}`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server.URL)

	servers, err := client.ListAuthorizationServers(t.Context(), "example.okta.com", "test-access-token")
	require.NoError(t, err)
	require.Equal(t, []okta.AuthorizationServer{{ID: "server-other", Name: "other"}, {ID: "server-default", Name: "default"}}, servers)
	require.NoError(t, client.CreateGroupsClaim(t.Context(), "example.okta.com", "test-access-token", "server-default"))
	require.JSONEq(t, `{
		"alwaysIncludeInToken":true,
		"claimType":"IDENTITY",
		"conditions":{"scopes":[]},
		"group_filter_type":"REGEX",
		"name":"groups",
		"status":"ACTIVE",
		"value":".*",
		"valueType":"GROUPS"
	}`, string(<-claimBodies))
}

func TestCreateGroupsClaimDoesNotRetry(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errorCode":"E0000009","errorSummary":"temporary failure"}`))
	}))
	t.Cleanup(server.Close)

	err := newTestClientWithRetries(t, server.URL, 3).CreateGroupsClaim(t.Context(), "example.okta.com", "test-access-token", "server-default")
	require.Error(t, err)
	require.Equal(t, int64(1), requests.Load())
}

func TestEnsureSignInClaimsCreatesRepairsVerifiesAndIsIdempotent(t *testing.T) {
	t.Parallel()

	type mutation struct {
		method string
		path   string
		body   []byte
	}
	claims := []map[string]any{
		{"id": "groups-claim", "alwaysIncludeInToken": true, "claimType": "IDENTITY", "conditions": map[string]any{"scopes": []string{}}, "group_filter_type": "REGEX", "name": "groups", "status": "ACTIVE", "value": ".*", "valueType": "GROUPS"},
		{"id": "given-name-claim", "alwaysIncludeInToken": false, "claimType": "IDENTITY", "conditions": map[string]any{"scopes": []string{"profile"}}, "name": "given_name", "status": "INACTIVE", "value": "user.displayName", "valueType": "EXPRESSION"},
		{"id": "email-claim", "alwaysIncludeInToken": true, "claimType": "IDENTITY", "conditions": map[string]any{"scopes": []string{}}, "name": "email", "status": "ACTIVE", "value": "user.email", "valueType": "EXPRESSION"},
	}
	var gets atomic.Int64
	var mu sync.Mutex
	var mutations []mutation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("Accept") != "application/json" {
			http.Error(w, "invalid authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/authorizationServers/auth-server-1/claims":
			gets.Add(1)
			mu.Lock()
			defer mu.Unlock()
			_ = json.NewEncoder(w).Encode(claims)
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/authorizationServers/auth-server-1/claims/given-name-claim":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			mu.Lock()
			mutations = append(mutations, mutation{method: r.Method, path: r.URL.Path, body: body})
			claims[1] = map[string]any{"id": "given-name-claim", "alwaysIncludeInToken": true, "claimType": "IDENTITY", "conditions": map[string]any{"scopes": []string{}}, "name": "given_name", "status": "ACTIVE", "value": "user.firstName", "valueType": "EXPRESSION"}
			mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"given-name-claim"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/authorizationServers/auth-server-1/claims":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			mu.Lock()
			mutations = append(mutations, mutation{method: r.Method, path: r.URL.Path, body: body})
			claims = append(claims, map[string]any{"id": "family-name-claim", "alwaysIncludeInToken": true, "claimType": "IDENTITY", "conditions": map[string]any{"scopes": []string{}}, "name": "family_name", "status": "ACTIVE", "value": "user.lastName", "valueType": "EXPRESSION"})
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"family-name-claim"}`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server.URL)

	names, err := client.EnsureSignInClaims(t.Context(), "example.okta.com", "test-access-token", "auth-server-1")
	require.NoError(t, err)
	require.Equal(t, []string{"groups", "given_name", "family_name", "email"}, names)
	secondNames, err := client.EnsureSignInClaims(t.Context(), "example.okta.com", "test-access-token", "auth-server-1")
	require.NoError(t, err)
	require.Equal(t, names, secondNames)
	require.Equal(t, int64(4), gets.Load())
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, mutations, 2)
	require.Equal(t, http.MethodPut, mutations[0].method)
	require.Equal(t, "/api/v1/authorizationServers/auth-server-1/claims/given-name-claim", mutations[0].path)
	require.JSONEq(t, `{"alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"name":"given_name","status":"ACTIVE","value":"user.firstName","valueType":"EXPRESSION"}`, string(mutations[0].body))
	require.Equal(t, http.MethodPost, mutations[1].method)
	require.Equal(t, "/api/v1/authorizationServers/auth-server-1/claims", mutations[1].path)
	require.JSONEq(t, `{"alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"name":"family_name","status":"ACTIVE","value":"user.lastName","valueType":"EXPRESSION"}`, string(mutations[1].body))
}

func TestEnsureSignInClaimsCreatesExactPayloadsInDeterministicOrder(t *testing.T) {
	t.Parallel()

	var gets atomic.Int64
	var posted [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			if gets.Add(1) == 1 {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[
				{"id":"groups","alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"group_filter_type":"REGEX","name":"groups","status":"ACTIVE","value":".*","valueType":"GROUPS"},
				{"id":"given_name","alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"name":"given_name","status":"ACTIVE","value":"user.firstName","valueType":"EXPRESSION"},
				{"id":"family_name","alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"name":"family_name","status":"ACTIVE","value":"user.lastName","valueType":"EXPRESSION"},
				{"id":"email","alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"name":"email","status":"ACTIVE","value":"user.email","valueType":"EXPRESSION"}
			]`))
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		posted = append(posted, body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"created-claim"}`))
	}))
	t.Cleanup(server.Close)

	names, err := newTestClient(t, server.URL).EnsureSignInClaims(t.Context(), "example.okta.com", "test-access-token", "auth-server-1")
	require.NoError(t, err)
	require.Equal(t, []string{"groups", "given_name", "family_name", "email"}, names)
	require.Len(t, posted, 4)
	require.JSONEq(t, `{"alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"group_filter_type":"REGEX","name":"groups","status":"ACTIVE","value":".*","valueType":"GROUPS"}`, string(posted[0]))
	require.JSONEq(t, `{"alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"name":"given_name","status":"ACTIVE","value":"user.firstName","valueType":"EXPRESSION"}`, string(posted[1]))
	require.JSONEq(t, `{"alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"name":"family_name","status":"ACTIVE","value":"user.lastName","valueType":"EXPRESSION"}`, string(posted[2]))
	require.JSONEq(t, `{"alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"name":"email","status":"ACTIVE","value":"user.email","valueType":"EXPRESSION"}`, string(posted[3]))
}

func TestEnsureSignInClaimsFailsWhenVerificationDoesNotMatch(t *testing.T) {
	t.Parallel()

	var gets atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			gets.Add(1)
			_, _ = w.Write([]byte(`[
				{"id":"groups-claim","alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"group_filter_type":"REGEX","name":"groups","status":"ACTIVE","value":".*","valueType":"GROUPS"},
				{"id":"given-claim","alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"name":"given_name","status":"ACTIVE","value":"user.firstName","valueType":"EXPRESSION"},
				{"id":"family-claim","alwaysIncludeInToken":true,"claimType":"IDENTITY","conditions":{"scopes":[]},"name":"family_name","status":"ACTIVE","value":"user.lastName","valueType":"EXPRESSION"}
			]`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"email-claim"}`))
	}))
	t.Cleanup(server.Close)

	names, err := newTestClient(t, server.URL).EnsureSignInClaims(t.Context(), "example.okta.com", "test-access-token", "auth-server-1")
	require.ErrorContains(t, err, `claim "email" did not match after update`)
	require.Nil(t, names)
	require.Equal(t, int64(2), gets.Load())
}

func TestEnsureSignInClaimsMutationDoesNotRetry(t *testing.T) {
	t.Parallel()

	var posts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		posts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errorCode":"E0000009","errorSummary":"temporary failure"}`))
	}))
	t.Cleanup(server.Close)

	_, err := newTestClientWithRetries(t, server.URL, 3).EnsureSignInClaims(t.Context(), "example.okta.com", "test-access-token", "auth-server-1")
	require.Error(t, err)
	require.Equal(t, int64(1), posts.Load())
}

func TestEnsureOktaConfigurationValidatesArguments(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "http://127.0.0.1:1")
	require.Error(t, client.EnsureOIDCApplicationRedirectURI(t.Context(), " ", "token", "app", "https://example.com/callback"))
	require.Error(t, client.EnsureAuthorizationServerPolicyClient(t.Context(), "tenant", "", "server", "app"))
	names, err := client.EnsureSignInClaims(t.Context(), "tenant", "token", "")
	require.Error(t, err)
	require.Nil(t, names)
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
		_, _ = w.Write([]byte(`{"id":"app-1","status":"ACTIVE","label":"Speakeasy sign-in","credentials":{"oauthClient":{"client_id":"client-1","client_secret":"` + responseSecret + `"}}}`))
	}))
	t.Cleanup(server.Close)

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	app, err := newTestClientWithLogger(t, server.URL, logger).CreateOIDCApplication(t.Context(), "example.okta.com", "test-access-token", okta.CreateOIDCApplicationInput{
		RedirectURIs: []string{"https://auth.example.com/sso/callback", "https://app.example.com/oauth/callback"},
		ClientSecret: "caller-minted-secret",
	})
	require.NoError(t, err)
	require.Equal(t, okta.Application{ID: "app-1", Status: "ACTIVE", Label: "Speakeasy sign-in", ClientID: "client-1", SignOnMode: "", SignOnURL: "", LogoURL: ""}, app)

	request := <-requests
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "/api/v1/apps", request.URL.Path)
	require.Equal(t, "Bearer test-access-token", request.Header.Get("Authorization"))
	require.Equal(t, "application/json", request.Header.Get("Content-Type"))
	body := <-bodies
	require.JSONEq(t, `{
		"name":"oidc_client",
		"label":"Speakeasy sign-in",
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

func TestFindActiveApplicationByLabelReturnsTransientSecret(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/apps" || r.URL.Query().Get("limit") != "200" || r.URL.Query().Has("filter") || r.Header.Get("Authorization") != "Bearer test-access-token" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"inactive-app","status":"INACTIVE","label":"Speakeasy sign-in","credentials":{"oauthClient":{"client_id":"inactive-client","client_secret":"inactive-secret"}}},
			{"id":"active-app","status":"ACTIVE","label":"Speakeasy sign-in","credentials":{"oauthClient":{"client_id":"active-client","client_secret":"active-secret"}}}
		]`))
	}))
	t.Cleanup(server.Close)

	app, secret, found, err := newTestClient(t, server.URL).FindActiveApplicationByLabel(t.Context(), "example.okta.com", "test-access-token", okta.SignInApplicationLabel)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, okta.Application{ID: "active-app", Status: "ACTIVE", Label: "Speakeasy sign-in", ClientID: "active-client", SignOnMode: "", SignOnURL: "", LogoURL: ""}, app)
	require.Equal(t, "active-secret", secret)
	require.NotContains(t, fmt.Sprintf("%+v", app), secret)
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
	require.Equal(t, okta.Application{ID: "app-123", Status: "INACTIVE", Label: "Speakeasy", ClientID: "client-123", SignOnMode: "", SignOnURL: "", LogoURL: ""}, app)
	require.NotContains(t, fmt.Sprintf("%+v", app), "discard-me")
}

func TestEnsureOIDCApplicationRedirectURIPreservesWritableFieldsAndIsIdempotent(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	var puts atomic.Int64
	var putBody []byte
	redirectURIs := []string{"https://existing.example.com/callback"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/api/v1/apps/app-instance-1" || r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("Accept") != "application/json" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "app-instance-1", "status": "ACTIVE", "created": "2026-01-01T00:00:00Z", "lastUpdated": "2026-01-02T00:00:00Z",
				"name": "oidc_client", "label": "Speakeasy sign-in", "signOnMode": "OPENID_CONNECT",
				"accessibility": map[string]any{"selfService": false}, "features": []string{}, "profile": map[string]any{"owner": "platform"},
				"universalLogout": map[string]any{"protocol": "OIDC_FRONT_CHANNEL"}, "visibility": map[string]any{"autoSubmitToolbar": false},
				"credentials": map[string]any{"oauthClient": map[string]any{"client_id": "oauth-client-1", "client_secret": "must-not-leave", "token_endpoint_auth_method": "client_secret_post", "autoKeyRotation": true}},
				"settings": map[string]any{
					"app":         map[string]any{"logoURI": "https://example.com/logo.png"},
					"oauthClient": map[string]any{"application_type": "web", "grant_types": []string{"authorization_code", "refresh_token"}, "redirect_uris": redirectURIs, "response_types": []string{"code"}},
				},
				"_links": map[string]any{"self": map[string]any{"href": "https://example.okta.com/api/v1/apps/app-instance-1"}},
			})
		case http.MethodPut:
			puts.Add(1)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			putBody = body
			redirectURIs = []string{"https://existing.example.com/callback", "https://new.example.com/callback"}
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server.URL)

	require.NoError(t, client.EnsureOIDCApplicationRedirectURI(t.Context(), "example.okta.com", "test-access-token", "app-instance-1", "https://new.example.com/callback"))
	require.NoError(t, client.EnsureOIDCApplicationRedirectURI(t.Context(), "example.okta.com", "test-access-token", "app-instance-1", "https://new.example.com/callback"))
	require.Equal(t, int64(3), requests.Load())
	require.Equal(t, int64(1), puts.Load())
	require.JSONEq(t, `{
		"name":"oidc_client",
		"label":"Speakeasy sign-in",
		"signOnMode":"OPENID_CONNECT",
		"accessibility":{"selfService":false},
		"features":[],
		"profile":{"owner":"platform"},
		"universalLogout":{"protocol":"OIDC_FRONT_CHANNEL"},
		"visibility":{"autoSubmitToolbar":false},
		"credentials":{"oauthClient":{"client_id":"oauth-client-1","token_endpoint_auth_method":"client_secret_post","autoKeyRotation":true}},
		"settings":{"app":{"logoURI":"https://example.com/logo.png"},"oauthClient":{"application_type":"web","grant_types":["authorization_code","refresh_token"],"redirect_uris":["https://existing.example.com/callback","https://new.example.com/callback"],"response_types":["code"]}}
	}`, string(putBody))
	require.NotContains(t, string(putBody), "must-not-leave")
}

func TestEnsureOIDCApplicationRedirectURIPutDoesNotRetry(t *testing.T) {
	t.Parallel()

	var puts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"name":"oidc_client","label":"Sign-in","signOnMode":"OPENID_CONNECT","credentials":{"oauthClient":{"client_secret":"discard-me"}},"settings":{"oauthClient":{"redirect_uris":[]}}}`))
			return
		}
		puts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errorCode":"E0000009","errorSummary":"temporary failure"}`))
	}))
	t.Cleanup(server.Close)

	err := newTestClientWithRetries(t, server.URL, 3).EnsureOIDCApplicationRedirectURI(t.Context(), "example.okta.com", "test-access-token", "app-instance-1", "https://new.example.com/callback")
	require.Error(t, err)
	require.Equal(t, int64(1), puts.Load())
}

func TestEnsureAuthorizationServerPolicyClientPreservesPolicyAndIsIdempotent(t *testing.T) {
	t.Parallel()

	var gets atomic.Int64
	var puts atomic.Int64
	var putBody []byte
	include := []string{"oauth-client-id-is-not-the-app-id"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("Accept") != "application/json" {
			http.Error(w, "invalid authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/authorizationServers/auth-server-1/policies":
			gets.Add(1)
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "inactive", "type": "OAUTH_AUTHORIZATION_POLICY", "status": "INACTIVE", "name": "Default Policy", "description": "ignored", "priority": 1, "conditions": map[string]any{"clients": map[string]any{"include": []string{"app-instance-1"}}}},
				map[string]any{"id": "default-policy", "type": "OAUTH_AUTHORIZATION_POLICY", "status": "ACTIVE", "name": "Default Policy", "description": "Default access", "priority": 2, "conditions": map[string]any{"clients": map[string]any{"include": include, "exclude": []string{"blocked-app"}}, "scopes": map[string]any{"include": []string{"openid"}}}},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/authorizationServers/auth-server-1/policies/default-policy":
			puts.Add(1)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			putBody = body
			include = []string{"oauth-client-id-is-not-the-app-id", "app-instance-1"}
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server.URL)

	require.NoError(t, client.EnsureAuthorizationServerPolicyClient(t.Context(), "example.okta.com", "test-access-token", "auth-server-1", "app-instance-1"))
	require.NoError(t, client.EnsureAuthorizationServerPolicyClient(t.Context(), "example.okta.com", "test-access-token", "auth-server-1", "app-instance-1"))
	require.Equal(t, int64(2), gets.Load())
	require.Equal(t, int64(1), puts.Load())
	require.JSONEq(t, `{
		"type":"OAUTH_AUTHORIZATION_POLICY",
		"status":"ACTIVE",
		"name":"Default Policy",
		"description":"Default access",
		"priority":2,
		"conditions":{"clients":{"include":["oauth-client-id-is-not-the-app-id","app-instance-1"],"exclude":["blocked-app"]},"scopes":{"include":["openid"]}}
	}`, string(putBody))
}

func TestEnsureAuthorizationServerPolicyClientAcceptsAllClients(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"other-policy","type":"OAUTH_AUTHORIZATION_POLICY","status":"ACTIVE","name":"Other Policy","description":"","priority":1,"conditions":{"clients":{"include":["ALL_CLIENTS"]}}}]`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, newTestClient(t, server.URL).EnsureAuthorizationServerPolicyClient(t.Context(), "example.okta.com", "test-access-token", "auth-server-1", "app-instance-1"))
	require.Equal(t, int64(1), requests.Load())
}

func TestEnsureAuthorizationServerPolicyClientRejectsAmbiguousDefault(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"default-1","type":"OAUTH_AUTHORIZATION_POLICY","status":"ACTIVE","name":"Default Policy","description":"","priority":1,"conditions":{"clients":{"include":[]}}},
			{"id":"default-2","type":"OAUTH_AUTHORIZATION_POLICY","status":"ACTIVE","name":"Default Policy","description":"","priority":2,"conditions":{"clients":{"include":[]}}}
		]`))
	}))
	t.Cleanup(server.Close)

	err := newTestClient(t, server.URL).EnsureAuthorizationServerPolicyClient(t.Context(), "example.okta.com", "test-access-token", "auth-server-1", "app-instance-1")
	require.ErrorContains(t, err, "expected exactly one active Default Policy, found 2")
	require.NotContains(t, err.Error(), "test-access-token")
}

func TestEnsureAuthorizationServerPolicyClientPutDoesNotRetry(t *testing.T) {
	t.Parallel()

	var puts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"id":"default-policy","type":"OAUTH_AUTHORIZATION_POLICY","status":"ACTIVE","name":"Default Policy","description":"","priority":1,"conditions":{"clients":{"include":[]}}}]`))
			return
		}
		puts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errorCode":"E0000009","errorSummary":"temporary failure"}`))
	}))
	t.Cleanup(server.Close)

	err := newTestClientWithRetries(t, server.URL, 3).EnsureAuthorizationServerPolicyClient(t.Context(), "example.okta.com", "test-access-token", "auth-server-1", "app-instance-1")
	require.Error(t, err)
	require.Equal(t, int64(1), puts.Load())
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
	require.Equal(t, okta.Application{ID: "matching-app", Status: "ACTIVE", Label: "Speakeasy", ClientID: clientID, SignOnMode: "", SignOnURL: "", LogoURL: ""}, app)
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
	require.Equal(t, okta.Group{ID: "everyone-group", Name: "Everyone", Type: "BUILT_IN"}, group)
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
