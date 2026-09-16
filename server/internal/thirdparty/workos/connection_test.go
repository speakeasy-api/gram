package workos_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestClientCreateOIDCConnectionSendsExactPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan *http.Request, 1)
	bodies := make(chan []byte, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		requests <- r.Clone(t.Context())
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"conn_example","organization_id":"org_example","connection_type":"GenericOIDC","name":"Okta","state":"active","callback_endpoint":"https://api.workos.com/sso/callback/conn_example","oidc_options":{"discovery_endpoint":"https://example.okta.com/.well-known/openid-configuration","redirect_uri":"https://api.workos.com/sso/callback/conn_example"},"created_at":"2026-09-15T00:00:00Z","updated_at":"2026-09-15T00:00:00Z"}`))
	})

	connection, err := newClientWithHandler(t, handler).CreateOIDCConnection(t.Context(), workos.CreateOIDCConnectionInput{
		OrganizationID:    "org_example",
		Name:              "Okta",
		DiscoveryEndpoint: "https://example.okta.com/.well-known/openid-configuration",
		ClientID:          "client_example",
		ClientSecret:      "secret_example",
	})
	require.NoError(t, err)
	require.Equal(t, workos.Connection{
		ID:                    "conn_example",
		OrganizationID:        "org_example",
		ConnectionType:        "GenericOIDC",
		Name:                  "Okta",
		State:                 "active",
		CallbackEndpoint:      "https://api.workos.com/sso/callback/conn_example",
		OIDCDiscoveryEndpoint: "https://example.okta.com/.well-known/openid-configuration",
		OIDCRedirectURI:       "https://api.workos.com/sso/callback/conn_example",
		CreatedAt:             "2026-09-15T00:00:00Z",
		UpdatedAt:             "2026-09-15T00:00:00Z",
	}, connection)

	request := <-requests
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "/connections", request.URL.Path)
	require.Equal(t, "Bearer test-api-key", request.Header.Get("Authorization"))
	require.JSONEq(t, `{
		"organization_id":"org_example",
		"name":"Okta",
		"connection_type":"GenericOIDC",
		"oidc_options":{
			"discovery_endpoint":"https://example.okta.com/.well-known/openid-configuration",
			"client_id":"client_example",
			"client_secret":"secret_example",
			"token_authentication_method":"client_secret_post",
			"pkce":true
		},
		"attribute_maps":{
			"standard_attributes":{"email":"email","first_name":"given_name","last_name":"family_name","groups":"groups"}
		}
	}`, string(<-bodies))
}

func TestClientCreateOIDCConnectionRedactsReflectedSecret(t *testing.T) {
	t.Parallel()

	const secret = "secret-must-not-escape"
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "rejected " + secret})
	})
	_, err := newClientWithHandler(t, handler).CreateOIDCConnection(t.Context(), workos.CreateOIDCConnectionInput{
		OrganizationID:    "org_example",
		Name:              "Okta",
		DiscoveryEndpoint: "https://example.okta.com/.well-known/openid-configuration",
		ClientID:          "client_example",
		ClientSecret:      secret,
	})
	var apiErr *workos.APIError
	require.ErrorAs(t, err, &apiErr)
	require.NotContains(t, apiErr.Body, secret)
	require.NotContains(t, err.Error(), secret)
	require.Contains(t, apiErr.Body, "[redacted]")
}

func TestClientCreateOIDCConnectionDoesNotRetryAndRedactsEscapedSecret(t *testing.T) {
	t.Parallel()

	const secret = "secret-with-\"quote\\slash"
	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "rejected " + secret})
	})
	_, err := newClientWithHandler(t, handler).CreateOIDCConnection(t.Context(), workos.CreateOIDCConnectionInput{
		OrganizationID:    "org_example",
		Name:              "Okta",
		DiscoveryEndpoint: "https://example.okta.com/.well-known/openid-configuration",
		ClientID:          "client_example",
		ClientSecret:      secret,
	})
	require.Equal(t, int64(1), calls.Load())
	var apiErr *workos.APIError
	require.ErrorAs(t, err, &apiErr)
	require.NotContains(t, apiErr.Body, secret)
	require.NotContains(t, apiErr.Body, `secret-with-\"quote\\slash`)
	require.Contains(t, apiErr.Body, "[redacted]")
}

func TestClientGetConnectionReadsByID(t *testing.T) {
	t.Parallel()

	requests := make(chan *http.Request, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(t.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"conn_example","organization_id":"org_example","connection_type":"GenericOIDC","name":"Okta","state":"draft","callback_endpoint":"https://api.workos.com/sso/callback/legacy","oidc_options":{"discovery_endpoint":"https://example.okta.com/.well-known/openid-configuration","redirect_uri":"https://api.workos.com/sso/callback/authoritative"},"created_at":"2026-09-15T00:00:00Z","updated_at":"2026-09-15T01:00:00Z"}`))
	})
	connection, err := newClientWithHandler(t, handler).GetConnection(t.Context(), "conn_example")
	require.NoError(t, err)
	request := <-requests
	require.Equal(t, http.MethodGet, request.Method)
	require.Equal(t, "/connections/conn_example", request.URL.Path)
	require.Equal(t, workos.Connection{
		ID:                    "conn_example",
		OrganizationID:        "org_example",
		ConnectionType:        "GenericOIDC",
		Name:                  "Okta",
		State:                 "draft",
		CallbackEndpoint:      "https://api.workos.com/sso/callback/legacy",
		OIDCDiscoveryEndpoint: "https://example.okta.com/.well-known/openid-configuration",
		OIDCRedirectURI:       "https://api.workos.com/sso/callback/authoritative",
		CreatedAt:             "2026-09-15T00:00:00Z",
		UpdatedAt:             "2026-09-15T01:00:00Z",
	}, connection)
}

func TestClientUpdateOIDCConnectionDiscoveryEndpointSendsExactRequest(t *testing.T) {
	t.Parallel()

	requests := make(chan *http.Request, 1)
	bodies := make(chan []byte, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		requests <- r.Clone(t.Context())
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"conn/example","organization_id":"org_example","connection_type":"GenericOIDC","name":"Okta","state":"active","callback_endpoint":"https://api.workos.com/sso/callback/legacy","oidc_options":{"discovery_endpoint":"https://new.example.com/.well-known/openid-configuration","redirect_uri":"https://api.workos.com/sso/callback/authoritative"},"created_at":"2026-09-15T00:00:00Z","updated_at":"2026-09-15T02:00:00Z"}`))
	})

	connection, err := newClientWithHandler(t, handler).UpdateOIDCConnectionDiscoveryEndpoint(
		t.Context(),
		"conn/example",
		"https://new.example.com/.well-known/openid-configuration",
	)
	require.NoError(t, err)
	require.Equal(t, workos.Connection{
		ID:                    "conn/example",
		OrganizationID:        "org_example",
		ConnectionType:        "GenericOIDC",
		Name:                  "Okta",
		State:                 "active",
		CallbackEndpoint:      "https://api.workos.com/sso/callback/legacy",
		OIDCDiscoveryEndpoint: "https://new.example.com/.well-known/openid-configuration",
		OIDCRedirectURI:       "https://api.workos.com/sso/callback/authoritative",
		CreatedAt:             "2026-09-15T00:00:00Z",
		UpdatedAt:             "2026-09-15T02:00:00Z",
	}, connection)

	request := <-requests
	require.Equal(t, http.MethodPatch, request.Method)
	require.Equal(t, "/connections/conn%2Fexample", request.URL.EscapedPath())
	require.Equal(t, "Bearer test-api-key", request.Header.Get("Authorization"))
	require.JSONEq(t, `{"oidc_options":{"discovery_endpoint":"https://new.example.com/.well-known/openid-configuration"}}`, string(<-bodies))
}

func TestClientUpdateOIDCConnectionDiscoveryEndpointRequiresConnectionID(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	})

	_, err := newClientWithHandler(t, handler).UpdateOIDCConnectionDiscoveryEndpoint(t.Context(), "  ", "https://example.com/.well-known/openid-configuration")
	require.EqualError(t, err, "update WorkOS OIDC connection: connection ID is required")
	require.Equal(t, int64(0), calls.Load())
}

func TestClientUpdateOIDCConnectionDiscoveryEndpointRequiresDiscoveryEndpoint(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	})

	_, err := newClientWithHandler(t, handler).UpdateOIDCConnectionDiscoveryEndpoint(t.Context(), "conn_example", "\t")
	require.EqualError(t, err, "update WorkOS OIDC connection: discovery endpoint is required")
	require.Equal(t, int64(0), calls.Load())
}

func TestClientUpdateOIDCConnectionDiscoveryEndpointDoesNotRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"try again"}`))
	})

	_, err := newClientWithHandler(t, handler).UpdateOIDCConnectionDiscoveryEndpoint(
		t.Context(),
		"conn_example",
		"https://example.com/.well-known/openid-configuration",
	)
	require.Error(t, err)
	require.Equal(t, int64(1), calls.Load())
}

func TestStubClientUpdateOIDCConnectionDiscoveryEndpoint(t *testing.T) {
	t.Parallel()

	client := workos.NewStubClient()
	created, err := client.CreateOIDCConnection(t.Context(), workos.CreateOIDCConnectionInput{
		OrganizationID:    "org_example",
		Name:              "Okta",
		DiscoveryEndpoint: "https://old.example.com/.well-known/openid-configuration",
		ClientID:          "client_example",
		ClientSecret:      "secret_example",
	})
	require.NoError(t, err)

	updated, err := client.UpdateOIDCConnectionDiscoveryEndpoint(t.Context(), created.ID, "https://new.example.com/.well-known/openid-configuration")
	require.NoError(t, err)
	require.Equal(t, "https://new.example.com/.well-known/openid-configuration", updated.OIDCDiscoveryEndpoint)

	stored, err := client.GetConnection(t.Context(), created.ID)
	require.NoError(t, err)
	require.Equal(t, updated, stored)
}

func TestIsConnectionsWriteUnavailableMatchesOnlyCapabilityError(t *testing.T) {
	t.Parallel()

	capabilityErr := &workos.APIError{
		Method:     http.MethodPost,
		Path:       "/connections",
		StatusCode: http.StatusNotFound,
		Body:       `{"message":"This endpoint is part of the Connections API migration capabilities, which are not enabled for your environment. Contact support@workos.com to enable them."}`,
	}
	require.True(t, workos.IsConnectionsWriteUnavailable(capabilityErr))
	require.False(t, workos.IsConnectionsWriteUnavailable(&workos.APIError{Method: http.MethodPost, Path: "/connections", StatusCode: http.StatusNotFound, Body: `{"message":"not found"}`}))
	require.False(t, workos.IsConnectionsWriteUnavailable(errors.New("network failure")))
}

func TestClientConnectionsAPIAvailableCachesEnabledCapability(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	requests := make(chan *http.Request, 1)
	bodies := make(chan []byte, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		requests <- r.Clone(t.Context())
		bodies <- body
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"connection_type_or_options_required"}`))
	})
	client := newClientWithHandler(t, handler)

	available, err := client.ConnectionsAPIAvailable(t.Context())
	require.NoError(t, err)
	require.True(t, available)
	request := <-requests
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "/connections", request.URL.Path)
	require.JSONEq(t, `{}`, string(<-bodies))
	available, err = client.ConnectionsAPIAvailable(t.Context())
	require.NoError(t, err)
	require.True(t, available)
	require.Equal(t, int64(1), calls.Load())
}

func TestClientConnectionsAPIAvailableRecognizesOrganizationValidation(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":[{"code":"organization_id should not be empty"}]}`))
	})

	available, err := newClientWithHandler(t, handler).ConnectionsAPIAvailable(t.Context())
	require.NoError(t, err)
	require.True(t, available)
}

func TestClientConnectionsAPIAvailableRejectsOtherValidation(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":[{"code":"organization_id is invalid"}]}`))
	})

	_, err := newClientWithHandler(t, handler).ConnectionsAPIAvailable(t.Context())
	require.Error(t, err)
}

func TestClientConnectionsAPIAvailableCachesDisabledCapability(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"This endpoint is part of the Connections API migration capabilities, which are not enabled for your environment. Contact support@workos.com to enable them."}`))
	})
	client := newClientWithHandler(t, handler)

	available, err := client.ConnectionsAPIAvailable(t.Context())
	require.NoError(t, err)
	require.False(t, available)
	available, err = client.ConnectionsAPIAvailable(t.Context())
	require.NoError(t, err)
	require.False(t, available)
	require.Equal(t, int64(1), calls.Load())
}

func TestClientConnectionsAPIAvailableDoesNotCacheFailures(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"try again"}`))
	})
	client := newClientWithHandler(t, handler)

	_, err := client.ConnectionsAPIAvailable(t.Context())
	require.Error(t, err)
	_, err = client.ConnectionsAPIAvailable(t.Context())
	require.Error(t, err)
	require.Equal(t, int64(2), calls.Load())
}

// newClientWithHandler builds a workos.Client pointed at an httptest server
// driven by the supplied handler. Used by tests that need to assert request
// paths or stub WorkOS responses without the broader fakeWorkOS state machine.
func newClientWithHandler(t *testing.T, handler http.Handler) *workos.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	return workos.NewClient(guardianPolicy, "test-api-key", workos.ClientOpts{
		Endpoint: srv.URL,
		ClientID: "test-client-id",
	})
}

// TestClient_ListConnections_HitsCorrectPath is a regression guard for the
// "wrong WorkOS endpoint" class of bug. Asserts the SDK targets /connections
// (NOT /sso/connections or any other variant) and forwards organization_id.
func TestClient_ListConnections_HitsCorrectPath(t *testing.T) {
	t.Parallel()
	var gotPath, gotOrgID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotOrgID = r.URL.Query().Get("organization_id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"list_metadata":{"before":"","after":""}}`))
	})

	client := newClientWithHandler(t, handler)
	_, err := client.ListConnections(context.Background(), "org_test_123")
	require.NoError(t, err)
	require.Equal(t, "/connections", gotPath)
	require.Equal(t, "org_test_123", gotOrgID)
}

// TestClient_ListDirectories_HitsCorrectPath is a regression guard for the
// production bug fixed by this PR. The previous raw-HTTP wrapper hit the
// non-existent /directory_sync/directories path and caused getOnboardingStatus
// to 500 in production. The correct WorkOS endpoint is /directories.
func TestClient_ListDirectories_HitsCorrectPath(t *testing.T) {
	t.Parallel()
	var gotPath, gotOrgID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotOrgID = r.URL.Query().Get("organization_id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"list_metadata":{"before":"","after":""}}`))
	})

	client := newClientWithHandler(t, handler)
	_, err := client.ListDirectories(context.Background(), "org_test_123")
	require.NoError(t, err)
	require.Equal(t, "/directories", gotPath)
	require.Equal(t, "org_test_123", gotOrgID)
}

func TestClient_ListConnections_DecodesResponse(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{"id":"conn_a","organization_id":"org_1","connection_type":"OktaSAML","name":"Okta","state":"active","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"},
				{"id":"conn_b","organization_id":"org_1","connection_type":"GoogleOAuth","name":"Google","state":"inactive","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"}
			],
			"list_metadata": {"before":"","after":""}
		}`))
	})

	client := newClientWithHandler(t, handler)
	conns, err := client.ListConnections(context.Background(), "org_1")
	require.NoError(t, err)
	require.Len(t, conns, 2)
	require.Equal(t, "conn_a", conns[0].ID)
	require.Equal(t, "active", conns[0].State)
	require.Equal(t, "OktaSAML", conns[0].ConnectionType)
	require.Equal(t, "inactive", conns[1].State)
}

func TestClient_ListConnectionsReadsEveryPage(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("limit") != "100" {
			http.Error(w, "unexpected limit", http.StatusBadRequest)
			return
		}
		if calls.Add(1) == 1 {
			if r.URL.Query().Get("after") != "" {
				http.Error(w, "unexpected first cursor", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"conn_a","organization_id":"org_1","connection_type":"GenericOIDC","name":"OIDC","state":"draft"}],"list_metadata":{"before":"","after":"next"}}`))
			return
		}
		if r.URL.Query().Get("after") != "next" {
			http.Error(w, "unexpected next cursor", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"conn_b","organization_id":"org_1","connection_type":"GenericOIDC","name":"OIDC","state":"active"}],"list_metadata":{"before":"","after":""}}`))
	})

	connections, err := newClientWithHandler(t, handler).ListConnections(t.Context(), "org_1")
	require.NoError(t, err)
	require.Equal(t, []string{"conn_a", "conn_b"}, []string{connections[0].ID, connections[1].ID})
	require.Equal(t, int64(2), calls.Load())
}

func TestClient_ListDirectories_DecodesResponse(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{"id":"dir_a","organization_id":"org_1","type":"okta scim v2.0","name":"Okta Dsync","state":"linked","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"},
				{"id":"dir_b","organization_id":"org_1","type":"azure scim v2.0","name":"Azure Dsync","state":"unlinked","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"}
			],
			"list_metadata": {"before":"","after":""}
		}`))
	})

	client := newClientWithHandler(t, handler)
	dirs, err := client.ListDirectories(context.Background(), "org_1")
	require.NoError(t, err)
	require.Len(t, dirs, 2)
	require.Equal(t, "dir_a", dirs[0].ID)
	require.Equal(t, "linked", dirs[0].State)
	require.Equal(t, "unlinked", dirs[1].State)
}

func TestClientListDirectoryGroupsReadsEveryPage(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/directory_groups" || r.URL.Query().Get("directory") != "dir_1" || r.URL.Query().Get("limit") != "100" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"group_a","directory_id":"dir_1","organization_id":"org_1","name":"Engineering"}],"list_metadata":{"before":"","after":"next"}}`))
			return
		}
		if r.URL.Query().Get("after") != "next" {
			http.Error(w, "unexpected cursor", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"group_b","directory_id":"dir_1","organization_id":"org_1","name":"Support"}],"list_metadata":{"before":"","after":""}}`))
	})

	groups, err := newClientWithHandler(t, handler).ListDirectoryGroups(t.Context(), "dir_1")
	require.NoError(t, err)
	require.Equal(t, []string{"group_a", "group_b"}, []string{groups[0].ID, groups[1].ID})
	require.Equal(t, int64(2), calls.Load())
}

func TestClient_ListConnections_NotFoundError(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found","error":"Not Found"}`))
	})

	client := newClientWithHandler(t, handler)
	_, err := client.ListConnections(context.Background(), "org_missing")
	require.Error(t, err)
	require.True(t, workos.IsNotFound(err), "404 from WorkOS should be detectable via IsNotFound, got %v", err)
}

func TestClient_ListDirectories_NotFoundError(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found","error":"Not Found"}`))
	})

	client := newClientWithHandler(t, handler)
	_, err := client.ListDirectories(context.Background(), "org_missing")
	require.Error(t, err)
	require.True(t, workos.IsNotFound(err), "404 from WorkOS should be detectable via IsNotFound, got %v", err)
}

func TestHasActiveConnection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []workos.Connection
		want bool
	}{
		{name: "empty slice", in: nil, want: false},
		{name: "only inactive", in: []workos.Connection{connectionWithState("inactive"), connectionWithState("draft")}, want: false},
		{name: "single active", in: []workos.Connection{connectionWithState("active")}, want: true},
		{name: "active among inactive", in: []workos.Connection{connectionWithState("draft"), connectionWithState("active"), connectionWithState("inactive")}, want: true},
		{name: "validating is not active", in: []workos.Connection{connectionWithState("validating")}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, workos.HasActiveConnection(tc.in))
		})
	}
}

func connectionWithState(state string) workos.Connection {
	return workos.Connection{
		ID:                    "",
		OrganizationID:        "",
		ConnectionType:        "",
		Name:                  "",
		State:                 state,
		CallbackEndpoint:      "",
		OIDCDiscoveryEndpoint: "",
		OIDCRedirectURI:       "",
		CreatedAt:             "",
		UpdatedAt:             "",
	}
}

func TestHasActiveDirectory(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []workos.Directory
		want bool
	}{
		{name: "empty slice", in: nil, want: false},
		{name: "only unlinked", in: []workos.Directory{{State: "unlinked"}}, want: false},
		{name: "single linked", in: []workos.Directory{{State: "linked"}}, want: true},
		{name: "linked among unlinked", in: []workos.Directory{{State: "unlinked"}, {State: "linked"}}, want: true},
		{name: "invalid_credentials is not linked", in: []workos.Directory{{State: "invalid_credentials"}}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, workos.HasActiveDirectory(tc.in))
		})
	}
}
