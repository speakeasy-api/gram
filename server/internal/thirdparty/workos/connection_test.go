package workos_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

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
		{name: "only inactive", in: []workos.Connection{{State: "inactive"}, {State: "draft"}}, want: false},
		{name: "single active", in: []workos.Connection{{State: "active"}}, want: true},
		{name: "active among inactive", in: []workos.Connection{{State: "draft"}, {State: "active"}, {State: "inactive"}}, want: true},
		{name: "validating is not active", in: []workos.Connection{{State: "validating"}}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, workos.HasActiveConnection(tc.in))
		})
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
