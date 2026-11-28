package workos_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func newTestWorkOSHTTPClient(t *testing.T, handler http.HandlerFunc) *workos.Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return workos.NewClient(policy, "sk_test_workos_key", workos.ClientOpts{
		Endpoint:    srv.URL,
		ClientID:    "client_test",
		RetryConfig: nil,
	})
}

func TestClient_ListUserSessionIDs_PaginatesAndFiltersActive(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	calls := 0
	client := newTestWorkOSHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/user_management/users/workos_user_1/sessions", r.URL.Path)
		assert.Equal(t, "Bearer sk_test_workos_key", r.Header.Get("Authorization"))

		calls++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"sess_active","status":"active"},{"id":"sess_revoked","status":"revoked"},{"id":"sess_no_status"}],"list_metadata":{"after":"cursor_2"}}`))
			return
		}
		assert.Equal(t, "cursor_2", r.URL.Query().Get("after"))
		_, _ = w.Write([]byte(`{"data":[{"id":"sess_expired","status":"expired"},{"id":"sess_active_upper","status":"ACTIVE"}],"list_metadata":{"after":""}}`))
	})

	ids, err := client.ListUserSessionIDs(ctx, "workos_user_1")
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, []string{"sess_active", "sess_no_status", "sess_active_upper"}, ids)
}

func TestClient_ListUserSessionIDs_ReturnsCollectedIDsWhenLaterPageFails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client := newTestWorkOSHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"sess_p1","status":"active"},{"id":"sess_p1_old","status":"revoked"}],"list_metadata":{"after":"cursor_2"}}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad cursor"}`))
	})

	ids, err := client.ListUserSessionIDs(ctx, "workos_user_1")
	require.Error(t, err)
	require.Equal(t, []string{"sess_p1"}, ids)
}

func TestStubClient_BlockValidationMirrorsRealClient(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	stub := workos.NewStubClient()

	_, err := stub.ListUserSessionIDs(ctx, "   ")
	require.Error(t, err)
	require.Error(t, stub.SetUserBlacklistedMetadata(ctx, " ", true))
	require.Error(t, stub.AddRadarEmailBlocklistEntry(ctx, "   "))

	require.NoError(t, stub.SetUserBlacklistedMetadata(ctx, "workos_u1", true))
	require.Equal(t, map[string]bool{"workos_u1": true}, stub.BlacklistedMetadata())

	require.NoError(t, stub.AddRadarEmailBlocklistEntry(ctx, "  Blocked@Example.Invalid "))
	require.Equal(t, []string{"blocked@example.invalid"}, stub.RadarBlockedEmails())
}

func TestClient_SetUserBlacklistedMetadata(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	var gotBody map[string]any
	client := newTestWorkOSHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/user_management/users/workos_user_2", r.URL.Path)
		assert.Equal(t, "Bearer sk_test_workos_key", r.Header.Get("Authorization"))
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"workos_user_2","email":"blocked@example.invalid"}`))
	})

	err := client.SetUserBlacklistedMetadata(ctx, "workos_user_2", true)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"blacklisted": "true"}, gotBody["metadata"])
}

func TestClient_AddRadarEmailBlocklistEntry(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	var gotEntry string
	client := newTestWorkOSHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/radar/lists/email/block", r.URL.Path)
		assert.Equal(t, "Bearer sk_test_workos_key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var payload struct {
			Entry string `json:"entry"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		gotEntry = payload.Entry
		w.WriteHeader(http.StatusCreated)
	})

	err := client.AddRadarEmailBlocklistEntry(ctx, "  Blocked@Example.Invalid ")
	require.NoError(t, err)
	require.Equal(t, "blocked@example.invalid", gotEntry)
}

func TestClient_AddRadarEmailBlocklistEntry_SurfacesAPIError(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client := newTestWorkOSHTTPClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"radar not enabled"}`))
	})

	err := client.AddRadarEmailBlocklistEntry(ctx, "blocked@example.invalid")
	require.Error(t, err)
	var apiErr *workos.APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
}
