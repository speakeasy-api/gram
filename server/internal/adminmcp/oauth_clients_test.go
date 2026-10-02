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
	"golang.org/x/crypto/bcrypt"
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
	}{
		{name: "confidential client", body: `{"client_name":"editor","redirect_uris":["http://localhost:5555/callback"],"token_endpoint_auth_method":"client_secret_basic"}`, status: http.StatusCreated},
		{name: "omitted authentication method", body: `{"client_name":"editor","redirect_uris":["http://localhost:5555/callback"]}`, status: http.StatusBadRequest, expectError: "invalid_client_metadata"},

		{name: "unsupported post authentication", body: `{"client_name":"editor","redirect_uris":["http://localhost:5555/callback"],"token_endpoint_auth_method":"client_secret_post"}`, status: http.StatusBadRequest, expectError: "invalid_client_metadata"},
		{name: "invalid redirect", body: `{"client_name":"editor","redirect_uris":["http://example.com/callback"],"token_endpoint_auth_method":"client_secret_basic"}`, status: http.StatusBadRequest, expectError: "invalid_redirect_uri"},
		{name: "unsupported grant", body: `{"client_name":"editor","redirect_uris":["http://localhost:5555/callback"],"token_endpoint_auth_method":"client_secret_basic","grant_types":["client_credentials"]}`, status: http.StatusBadRequest, expectError: "invalid_client_metadata"},
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
			secret, ok := payload["client_secret"].(string)
			require.True(t, ok)
			require.NotEmpty(t, secret)
			require.NoError(t, bcrypt.CompareHashAndPassword([]byte(store.client.SecretHash), []byte(secret)))
		})
	}
}

func TestStaffClientRegistrationPublicClientGetsNoSecret(t *testing.T) {
	t.Parallel()
	store := &recordingStaffClientStore{}
	handler := (&StaffOAuthClients{store: store}).RegisterHandler()
	request := httptest.NewRequest(http.MethodPost, "/admin-mcp/register", strings.NewReader(`{"client_name":"editor","redirect_uris":["http://127.0.0.1:5555/callback"],"grant_types":["authorization_code","refresh_token"],"token_endpoint_auth_method":"none"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Equal(t, store.client.ID, payload["client_id"])
	require.Equal(t, "none", payload["token_endpoint_auth_method"])
	require.NotContains(t, payload, "client_secret")
	require.NotContains(t, payload, "client_secret_expires_at")
	require.Empty(t, store.client.SecretHash)
}

func TestStaffClientRegistrationRejectsUnavailableAndWrongMethod(t *testing.T) {
	t.Parallel()
	handler := (&StaffOAuthClients{store: &recordingStaffClientStore{err: errors.New("storage failed")}}).RegisterHandler()
	request := httptest.NewRequest(http.MethodPost, "/admin-mcp/register", strings.NewReader(`{"client_name":"editor","redirect_uris":["http://localhost/callback"],"token_endpoint_auth_method":"client_secret_basic"}`))
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
