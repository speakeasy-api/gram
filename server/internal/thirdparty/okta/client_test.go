package okta_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
