package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingStaffClientStore struct {
	client staffOAuthClient
	err    error
}

func (s *recordingStaffClientStore) RegisterClient(_ context.Context, client staffOAuthClient) error {
	s.client = client
	return s.err
}

func (s *recordingStaffClientStore) GetClient(_ context.Context, id string) (staffOAuthClient, error) {
	if s.client.ID != id {
		return staffOAuthClient{}, errors.New("not found")
	}
	return s.client, nil
}

func TestStaffClientRegistration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		body        string
		status      int
		expectError string
		expectHash  bool
	}{
		{name: "confidential client", body: `{"client_name":"editor","redirect_uris":["http://localhost:5555/callback"]}`, status: http.StatusCreated, expectHash: true},
		{name: "public client", body: `{"client_name":"editor","redirect_uris":["http://localhost:5555/callback"],"token_endpoint_auth_method":"none"}`, status: http.StatusCreated},
		{name: "unsupported post authentication", body: `{"client_name":"editor","redirect_uris":["http://localhost:5555/callback"],"token_endpoint_auth_method":"client_secret_post"}`, status: http.StatusBadRequest, expectError: "invalid_client_metadata"},
		{name: "invalid redirect", body: `{"client_name":"editor","redirect_uris":["http://example.com/callback"]}`, status: http.StatusBadRequest, expectError: "invalid_redirect_uri"},
		{name: "unsupported grant", body: `{"client_name":"editor","redirect_uris":["http://localhost:5555/callback"],"grant_types":["client_credentials"]}`, status: http.StatusBadRequest, expectError: "invalid_client_metadata"},
		{name: "invalid json", body: `{`, status: http.StatusBadRequest, expectError: "invalid_client_metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &recordingStaffClientStore{}
			handler := (&StaffOAuthClients{store: store}).RegisterHandler()
			request := httptest.NewRequest(http.MethodPost, "/admin-mcp/register", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code)
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			var payload map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
			if tc.expectError != "" {
				require.Equal(t, tc.expectError, payload["error"])
				require.Empty(t, store.client.ID)
				return
			}
			require.Equal(t, store.client.ID, payload["client_id"])
			require.Equal(t, []any{"authorization_code", "refresh_token"}, payload["grant_types"])
			require.NotEmpty(t, store.client.ID)
			if tc.expectHash {
				require.NotEmpty(t, payload["client_secret"])
				require.NotContains(t, store.client.SecretHash, payload["client_secret"])
				require.NotEmpty(t, store.client.SecretHash)
			} else {
				require.Empty(t, store.client.SecretHash)
				require.NotContains(t, payload, "client_secret")
			}
		})
	}
}

func TestStaffClientRegistrationRejectsUnavailableAndWrongMethod(t *testing.T) {
	t.Parallel()
	handler := (&StaffOAuthClients{store: &recordingStaffClientStore{err: errors.New("storage failed")}}).RegisterHandler()
	request := httptest.NewRequest(http.MethodPost, "/admin-mcp/register", strings.NewReader(`{"client_name":"editor","redirect_uris":["http://localhost/callback"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.NotContains(t, response.Body.String(), "storage failed")

	request = httptest.NewRequest(http.MethodGet, "/admin-mcp/register", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
}
