package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

func TestWorkOSAdapterSetEmailVerifiedOnlySendsVerificationUpdate(t *testing.T) {
	t.Parallel()

	const userID = "user_01INVITEE"
	var requestBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPut)
		}
		if r.URL.Path != "/user_management/users/"+userID {
			t.Errorf("path = %q, want %q", r.URL.Path, "/user_management/users/"+userID)
		}
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("authorization = %q, want %q", r.Header.Get("Authorization"), "Bearer test")
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"user_01INVITEE","email":"invitee@example.com","first_name":"","last_name":"","created_at":"2026-05-18T00:00:00.000Z","updated_at":"2026-05-18T00:00:00.000Z","email_verified":true,"profile_picture_url":"","last_sign_in_at":"","external_id":"","metadata":{}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	client := usermanagement.NewClient("test")
	client.Endpoint = server.URL
	client.HTTPClient = server.Client()

	adapter := NewWorkOSAdapter(client)
	require.NoError(t, adapter.SetEmailVerified(context.Background(), userID))

	require.Equal(t, true, requestBody["email_verified"])
	for _, field := range []string{
		"email",
		"first_name",
		"last_name",
		"password",
		"password_hash",
		"password_hash_type",
		"external_id",
		"metadata",
	} {
		require.NotContains(t, requestBody, field)
	}
}

func TestWorkOSAdapterCodeExchangeErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		rejected bool
	}{
		{name: "invalid code", status: http.StatusBadRequest, body: `{"error":"invalid_grant","error_description":"code expired"}`, rejected: true},
		{name: "invalid client", status: http.StatusUnauthorized, body: `{"error":"invalid_client"}`},
		{name: "invalid request", status: http.StatusBadRequest, body: `{"error":"invalid_request"}`},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":"rate_limit_exceeded"}`},
		{name: "provider outage", status: http.StatusServiceUnavailable, body: `{"error":"temporarily_unavailable"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)
			client := usermanagement.NewClient("test")
			client.Endpoint = server.URL
			client.HTTPClient = server.Client()
			adapter := NewWorkOSAdapter(client)
			_, err := adapter.AuthenticateWithCode(t.Context(), "test-client", "test-code")
			require.Error(t, err)
			require.Equal(t, tc.rejected, errors.Is(err, ErrCodeRejected))
		})
	}
}
