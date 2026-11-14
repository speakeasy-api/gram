package resend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSend(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantError bool
	}{
		{"accepted", 200, `{"id":"email-123"}`, false},
		{"conflicting payload", 409, `{"message":"private recipient"}`, true},
		{"rate limited", 429, `{}`, true},
		{"unauthorized", 401, `{}`, true},
		{"invalid template", 422, `{}`, true},
		{"provider unavailable", 503, `{}`, true},
		{"malformed", 200, `broken`, true},
		{"missing id", 200, `{}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := SendInput{From: "platform@notifications.dev.speakeasy.com", To: []string{"person@example.com"}, ReplyTo: "platform@speakeasy.com", Template: Template{ID: "gram.v2.team_invite", Variables: map[string]string{"invite_link": "https://example.com/invite"}}, IdempotencyKey: "invite/123"}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/emails", r.URL.Path)
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				require.Equal(t, input.IdempotencyKey, r.Header.Get("Idempotency-Key"))
				var got SendInput
				require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				got.IdempotencyKey = input.IdempotencyKey
				require.Equal(t, input, got)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := &httpClient{transport: server.Client(), apiKey: "test-key", baseURL: server.URL}
			err := client.Send(t.Context(), input)
			if tc.wantError {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private recipient")
			} else {
				require.NoError(t, err)
				// Identical-key replays return the original successful response;
				// unlike payload conflicts, they remain successful.
				require.NoError(t, client.Send(t.Context(), input))
			}
		})
	}
}

func TestNoopClientSend(t *testing.T) {
	client := NewNoop()
	require.NotNil(t, client)
	require.NoError(t, client.Send(t.Context(), SendInput{
		From:     "platform@notifications.dev.speakeasy.com",
		To:       []string{"person@example.com"},
		Template: Template{ID: "gram.v2.team_invite"},
	}))
	require.NoError(t, client.Send(t.Context(), SendInput{}))
}
