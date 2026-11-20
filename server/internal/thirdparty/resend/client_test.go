package resend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSend(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			input := SendInput{From: "platform@notifications.dev.speakeasy.com", To: []string{"person@example.com"}, ReplyTo: "platform@speakeasy.com", Template: Template{ID: "gram.v2.team_invite", Variables: map[string]string{"invite_link": "https://example.com/invite"}}, IdempotencyKey: "invite/123"}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/emails", r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, input.IdempotencyKey, r.Header.Get("Idempotency-Key"))
				var got SendInput
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				got.IdempotencyKey = input.IdempotencyKey
				assert.Equal(t, input, got)
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
	t.Parallel()

	client := NewNoop()
	require.NotNil(t, client)
	require.NoError(t, client.Send(t.Context(), SendInput{
		From:     "platform@notifications.dev.speakeasy.com",
		To:       []string{"person@example.com"},
		Template: Template{ID: "gram.v2.team_invite"},
	}))
	require.NoError(t, client.Send(t.Context(), SendInput{}))
}

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestSendDeadline(t *testing.T) {
	t.Parallel()
	for _, callerTimeout := range []time.Duration{0, time.Second} {
		t.Run(callerTimeout.String(), func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			wantTimeout := 30 * time.Second
			if callerTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, callerTimeout)
				defer cancel()
				wantTimeout = callerTimeout
			}
			var contextErr func() error
			client := New(httpDoerFunc(func(req *http.Request) (*http.Response, error) {
				deadline, ok := req.Context().Deadline()
				require.True(t, ok)
				remaining := time.Until(deadline)
				require.Positive(t, remaining)
				require.LessOrEqual(t, remaining, wantTimeout)
				contextErr = req.Context().Err
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"email-123"}`))}, nil
			}), "test-key")
			require.NoError(t, client.Send(ctx, SendInput{}))
			require.ErrorIs(t, contextErr(), context.Canceled)
		})
	}
}

func TestSendHonorsCallerCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := New(httpDoerFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	}), "test-key")
	require.ErrorIs(t, client.Send(ctx, SendInput{}), context.Canceled)
}
