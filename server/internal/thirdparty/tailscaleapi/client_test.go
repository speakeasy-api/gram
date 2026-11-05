package tailscaleapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/tailscaleapi"
)

type recordedRequest struct {
	method string
	path   string
	body   map[string]any
}

// stubAPI is a minimal Tailscale API: an OAuth token endpoint plus whatever
// handler the test installs for the resource under test.
type stubAPI struct {
	mu            sync.Mutex
	requests      []recordedRequest
	tokenRequests int
	tokenStatus   int
	status        int
	response      any
	token         string
}

func (s *stubAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.tokenRequests++
		tokenStatus := s.tokenStatus
		s.mu.Unlock()
		if tokenStatus != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tokenStatus)
			_, _ = w.Write([]byte(`{"error":"temporarily_unavailable"}`))
			return
		}
		id, secret := r.FormValue("client_id"), r.FormValue("client_secret")
		if id == "" || secret == "" {
			user, pass, ok := r.BasicAuth()
			if ok {
				id, secret = user, pass
			}
		}
		if id != "cid" || secret != "csecret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": s.token, "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
			return
		}
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		s.mu.Lock()
		s.requests = append(s.requests, recordedRequest{method: r.Method, path: r.URL.Path, body: body})
		status, response := s.status, s.response
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if response != nil {
			_ = json.NewEncoder(w).Encode(response)
		}
	})
	return mux
}

func (s *stubAPI) tokenExchanges() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokenRequests
}

func (s *stubAPI) recorded() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedRequest(nil), s.requests...)
}

func newClient(t *testing.T, stub *stubAPI) *tailscaleapi.HTTPClient {
	t.Helper()
	server := httptest.NewServer(stub.handler())
	t.Cleanup(server.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	client, err := tailscaleapi.New(policy, server.URL)
	require.NoError(t, err)
	return client
}

var creds = tailscaleapi.Credentials{ClientID: "cid", ClientSecret: "csecret"}

func TestCreateAuthKeySendsOneShotTaggedKey(t *testing.T) {
	t.Parallel()
	stub := &stubAPI{mu: sync.Mutex{}, requests: nil, tokenRequests: 0, tokenStatus: 0, status: http.StatusOK, token: "tok", response: map[string]any{
		"id": "k123", "key": "tskey-auth-secret", "expires": time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	}}
	client := newClient(t, stub)

	key, err := client.CreateAuthKey(t.Context(), creds, tailscaleapi.AuthKeyRequest{Tag: "tag:speakeasy", Description: "slot 0", TTL: 7 * 24 * time.Hour})
	require.NoError(t, err)
	require.Equal(t, "k123", key.ID)
	require.Equal(t, "tskey-auth-secret", key.Key)
	require.Equal(t, 2030, key.Expires.Year())

	requests := stub.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].method)
	require.Equal(t, "/api/v2/tailnet/-/keys", requests[0].path)
	capabilities, ok := requests[0].body["capabilities"].(map[string]any)
	require.True(t, ok)
	devices, ok := capabilities["devices"].(map[string]any)
	require.True(t, ok)
	create, ok := devices["create"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, create["reusable"])
	require.Equal(t, false, create["ephemeral"])
	require.Equal(t, true, create["preauthorized"])
	require.Equal(t, []any{"tag:speakeasy"}, create["tags"])
	require.InDelta(t, float64(7*24*3600), requests[0].body["expirySeconds"], 0)
	require.Equal(t, "slot 0", requests[0].body["description"])
}

func TestCreateAuthKeyRejectedCredentials(t *testing.T) {
	t.Parallel()
	stub := &stubAPI{mu: sync.Mutex{}, requests: nil, tokenRequests: 0, tokenStatus: 0, status: http.StatusOK, token: "tok", response: nil}
	client := newClient(t, stub)

	_, err := client.CreateAuthKey(t.Context(), tailscaleapi.Credentials{ClientID: "cid", ClientSecret: "wrong"}, tailscaleapi.AuthKeyRequest{Tag: "tag:speakeasy", Description: "", TTL: time.Hour})
	require.ErrorIs(t, err, tailscaleapi.ErrUnauthorized)
	require.Empty(t, stub.recorded())
}

func TestAPIStatusesMapToDistinctErrors(t *testing.T) {
	t.Parallel()
	svc := tailscaleapi.Service{Name: "svc:speakeasy", Ports: []string{"tcp:443"}, Tags: []string{"tag:speakeasy"}, Comment: ""}
	for _, tc := range []struct {
		status int
		want   error
		not    []error
	}{
		{http.StatusForbidden, tailscaleapi.ErrForbidden, []error{tailscaleapi.ErrRateLimited, tailscaleapi.ErrUnavailable}},
		{http.StatusTooManyRequests, tailscaleapi.ErrRateLimited, []error{tailscaleapi.ErrUnauthorized, tailscaleapi.ErrUnavailable}},
		{http.StatusInternalServerError, tailscaleapi.ErrUnavailable, []error{tailscaleapi.ErrUnauthorized, tailscaleapi.ErrRateLimited}},
		{http.StatusServiceUnavailable, tailscaleapi.ErrUnavailable, []error{tailscaleapi.ErrUnauthorized, tailscaleapi.ErrRateLimited}},
		{http.StatusBadRequest, nil, []error{tailscaleapi.ErrUnauthorized, tailscaleapi.ErrRateLimited, tailscaleapi.ErrUnavailable}},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			t.Parallel()
			stub := &stubAPI{mu: sync.Mutex{}, requests: nil, tokenRequests: 0, tokenStatus: 0, status: tc.status, token: "tok", response: map[string]any{"message": "failed"}}
			err := newClient(t, stub).UpsertService(t.Context(), creds, svc)
			require.Error(t, err)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			}
			for _, other := range tc.not {
				require.NotErrorIs(t, err, other)
			}
		})
	}
}

func TestForbiddenStillMatchesUnauthorized(t *testing.T) {
	t.Parallel()
	stub := &stubAPI{mu: sync.Mutex{}, requests: nil, tokenRequests: 0, tokenStatus: 0, status: http.StatusForbidden, token: "tok", response: map[string]any{"message": "missing scope"}}
	err := newClient(t, stub).UpsertService(t.Context(), creds, tailscaleapi.Service{Name: "svc:speakeasy", Ports: []string{"tcp:443"}, Tags: []string{"tag:speakeasy"}, Comment: ""})
	require.ErrorIs(t, err, tailscaleapi.ErrUnauthorized, "callers that only ask whether the credentials work still match")
}

func TestUnauthorizedIsNotForbidden(t *testing.T) {
	t.Parallel()
	stub := &stubAPI{mu: sync.Mutex{}, requests: nil, tokenRequests: 0, tokenStatus: 0, status: http.StatusUnauthorized, token: "tok", response: map[string]any{"message": "unauthorized"}}
	err := newClient(t, stub).UpsertService(t.Context(), creds, tailscaleapi.Service{Name: "svc:speakeasy", Ports: []string{"tcp:443"}, Tags: []string{"tag:speakeasy"}, Comment: ""})
	require.ErrorIs(t, err, tailscaleapi.ErrUnauthorized)
	require.NotErrorIs(t, err, tailscaleapi.ErrForbidden)
}

func TestTokenEndpointOutageIsUnavailable(t *testing.T) {
	t.Parallel()
	stub := &stubAPI{mu: sync.Mutex{}, requests: nil, tokenRequests: 0, tokenStatus: http.StatusServiceUnavailable, status: http.StatusOK, token: "tok", response: nil}
	_, err := newClient(t, stub).CreateAuthKey(t.Context(), creds, tailscaleapi.AuthKeyRequest{Tag: "tag:speakeasy", Description: "", TTL: time.Hour})
	require.ErrorIs(t, err, tailscaleapi.ErrUnavailable)
	require.NotErrorIs(t, err, tailscaleapi.ErrUnauthorized)
}

func TestUnreachableAPIIsUnavailable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.NotFoundHandler())
	baseURL := server.URL
	server.Close()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	client, err := tailscaleapi.New(policy, baseURL)
	require.NoError(t, err)

	err = client.DeleteDevice(t.Context(), creds, "nABC")
	require.ErrorIs(t, err, tailscaleapi.ErrUnavailable)
}

func TestUpsertServiceBody(t *testing.T) {
	t.Parallel()
	stub := &stubAPI{mu: sync.Mutex{}, requests: nil, tokenRequests: 0, tokenStatus: 0, status: http.StatusOK, token: "tok", response: nil}
	client := newClient(t, stub)

	require.NoError(t, client.UpsertService(t.Context(), creds, tailscaleapi.Service{
		Name: "svc:speakeasy", Ports: []string{"tcp:443"}, Tags: []string{"tag:speakeasy"}, Comment: "Speakeasy private MCP ingress",
	}))
	requests := stub.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPut, requests[0].method)
	require.Equal(t, "/api/v2/tailnet/-/vip-services/svc:speakeasy", requests[0].path)
	require.Equal(t, "svc:speakeasy", requests[0].body["name"])
	require.Equal(t, []any{"tcp:443"}, requests[0].body["ports"])
	require.Equal(t, []any{"tag:speakeasy"}, requests[0].body["tags"])

	require.Error(t, client.UpsertService(t.Context(), creds, tailscaleapi.Service{Name: "speakeasy", Ports: []string{"tcp:443"}, Tags: nil, Comment: ""}))
}

func TestDeleteMissingResourcesSucceed(t *testing.T) {
	t.Parallel()
	stub := &stubAPI{mu: sync.Mutex{}, requests: nil, tokenRequests: 0, tokenStatus: 0, status: http.StatusNotFound, token: "tok", response: map[string]any{"message": "not found"}}
	client := newClient(t, stub)

	require.NoError(t, client.DeleteDevice(t.Context(), creds, "nABC"))
	require.NoError(t, client.DeleteService(t.Context(), creds, "svc:speakeasy"))
	requests := stub.recorded()
	require.Len(t, requests, 2)
	require.Equal(t, http.MethodDelete, requests[0].method)
	require.Equal(t, "/api/v2/device/nABC", requests[0].path)
	require.Equal(t, "/api/v2/tailnet/-/vip-services/svc:speakeasy", requests[1].path)

	require.Error(t, client.DeleteDevice(t.Context(), creds, " "))
	require.Error(t, client.DeleteService(t.Context(), creds, "speakeasy"))
}

func TestClientReusesTokensPerCredential(t *testing.T) {
	t.Parallel()
	stub := &stubAPI{mu: sync.Mutex{}, requests: nil, tokenRequests: 0, tokenStatus: 0, status: http.StatusOK, token: "tok", response: nil}
	client := newClient(t, stub)
	svc := tailscaleapi.Service{Name: "svc:speakeasy", Ports: []string{"tcp:443"}, Tags: []string{"tag:speakeasy"}, Comment: ""}

	require.NoError(t, client.UpsertService(t.Context(), creds, svc))
	require.NoError(t, client.UpsertService(t.Context(), creds, svc))
	require.Equal(t, 1, stub.tokenExchanges(), "the same OAuth client reuses its access token")
	require.Len(t, stub.recorded(), 2)

	// Credentials are rotated: the same client id with a new secret is a
	// different client and exchanges a token of its own.
	rotated := tailscaleapi.Credentials{ClientID: "cid", ClientSecret: "wrong"}
	err := client.UpsertService(t.Context(), rotated, svc)
	require.ErrorIs(t, err, tailscaleapi.ErrUnauthorized)
	afterRotation := stub.tokenExchanges()
	require.Greater(t, afterRotation, 1, "a different secret for the same client id is a new client")

	require.NoError(t, client.UpsertService(t.Context(), creds, svc))
	require.Equal(t, afterRotation, stub.tokenExchanges(), "the original client's token is still cached")
}

func TestFakeRecordsCalls(t *testing.T) {
	t.Parallel()
	fake := tailscaleapi.NewFake()
	key, err := fake.CreateAuthKey(t.Context(), creds, tailscaleapi.AuthKeyRequest{Tag: "tag:speakeasy", Description: "", TTL: time.Hour})
	require.NoError(t, err)
	require.NotEmpty(t, key.Key)
	require.NoError(t, fake.UpsertService(t.Context(), creds, tailscaleapi.Service{Name: "svc:speakeasy", Ports: []string{"tcp:443"}, Tags: nil, Comment: ""}))
	require.NoError(t, fake.DeleteDevice(t.Context(), creds, "n1"))
	require.Len(t, fake.Keys(), 1)
	require.Contains(t, fake.Services(), "svc:speakeasy")
	require.Equal(t, []string{"n1"}, fake.DeletedDevices())
	require.NoError(t, fake.DeleteService(t.Context(), creds, "svc:speakeasy"))
	require.Empty(t, fake.Services())
	_, err = fake.CreateAuthKey(t.Context(), tailscaleapi.Credentials{ClientID: "", ClientSecret: ""}, tailscaleapi.AuthKeyRequest{Tag: "tag:speakeasy", Description: "", TTL: time.Hour})
	require.ErrorIs(t, err, tailscaleapi.ErrUnauthorized)
}

func TestNewRequiresTLSOutsideLoopback(t *testing.T) {
	t.Parallel()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	_, err = tailscaleapi.New(policy, "http://api.tailscale.com")
	require.Error(t, err)
	_, err = tailscaleapi.New(policy, "https://api.tailscale.com")
	require.NoError(t, err)
	_, err = tailscaleapi.New(policy, "http://127.0.0.1:8080")
	require.NoError(t, err)
	_, err = tailscaleapi.New(policy, "http://localhost:8080")
	require.NoError(t, err)
	_, err = tailscaleapi.New(policy, "https://:443")
	require.Error(t, err, "a port without a hostname is not a usable base url")
}
