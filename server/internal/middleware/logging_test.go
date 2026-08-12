package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestLogSafeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no secrets untouched",
			in:   "/rpc/skills.list?limit=10",
			want: "/rpc/skills.list?limit=10",
		},
		{
			name: "token query parameter redacted",
			in:   "/rpc/skills.getShared?token=supersecret",
			want: "/rpc/skills.getShared?token=REDACTED",
		},
		{
			name: "token query parameter redacted among others",
			in:   "/rpc/chatSessions.revoke?a=1&token=supersecret",
			want: "/rpc/chatSessions.revoke?a=1&token=REDACTED",
		},
		{
			name: "shared skill path segment redacted",
			in:   "/shared/skills/supersecrettoken",
			want: "/shared/skills/REDACTED",
		},
		{
			name: "shared skill path with trailing segment redacted",
			in:   "/shared/skills/supersecrettoken/extra",
			want: "/shared/skills/REDACTED/extra",
		},
		{
			name: "shared skill path and token query both redacted",
			in:   "/shared/skills/supersecrettoken?token=alsosecret",
			want: "/shared/skills/REDACTED?token=REDACTED",
		},
		{
			name: "shared skills prefix without token untouched",
			in:   "/shared/skills/",
			want: "/shared/skills/",
		},
		{
			name: "unrelated shared path untouched",
			in:   "/shared/other/value",
			want: "/shared/other/value",
		},
		{
			name: "absolute referrer URL with tokenized path redacted",
			in:   "https://app.example.com/shared/skills/supersecrettoken",
			want: "https://app.example.com/shared/skills/REDACTED",
		},
		{
			name: "absolute referrer URL with token query redacted",
			in:   "https://app.example.com/page?token=supersecret",
			want: "https://app.example.com/page?token=REDACTED",
		},
		{
			name: "login email query parameter redacted",
			in:   "/rpc/auth.login?email=dev%40acme.corp",
			want: "/rpc/auth.login?email=REDACTED",
		},
		{
			name: "agent email query parameter redacted alongside other params",
			in:   "/rpc/agent.getPlugins?email=dev%40acme.corp&org_name=acme",
			want: "/rpc/agent.getPlugins?email=REDACTED&org_name=acme",
		},
		{
			name: "repeated email query parameter collapses to one redaction",
			in:   "/rpc/auth.login?email=first%40acme.corp&email=second%40acme.corp",
			want: "/rpc/auth.login?email=REDACTED",
		},
		{
			name: "chat search query parameter redacted",
			in:   "/rpc/chat.listChats?search=dev%40acme.corp&limit=10",
			want: "/rpc/chat.listChats?limit=10&search=REDACTED",
		},
		{
			name: "credential and personal data redacted together",
			in:   "/rpc/auth.login?email=dev%40acme.corp&token=supersecret",
			want: "/rpc/auth.login?email=REDACTED&token=REDACTED",
		},
		{
			name: "empty email query parameter still redacted",
			in:   "/rpc/auth.login?email=",
			want: "/rpc/auth.login?email=REDACTED",
		},
		{
			name: "unlisted parameter carrying an address untouched",
			in:   "/rpc/organizations.listMembers?user_id=abc123",
			want: "/rpc/organizations.listMembers?user_id=abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tt.in)
			require.NoError(t, err)
			require.Equal(t, tt.want, logSafeURL(u))
		})
	}
}

// Fixtures for the worst case the redaction has to survive: an address in the
// request query, a second one in the Referer, and a live token alongside both.
const (
	personalDataEmail   = "dev@acme.corp"
	refererEmail        = "referred@acme.corp"
	personalDataTarget  = "/rpc/auth.login?email=dev%40acme.corp&org_name=acme&token=supersecret"
	personalDataReferer = "https://app.example.com/signup?email=referred%40acme.corp"
)

// Covers the wiring rather than logSafeURL alone: the redacted URL, not the
// raw one, is what the middleware hands to slog. Asserting over the whole log
// buffer catches any attribute carrying the raw URL, not just url.original.
func TestHTTPLoggingMiddlewareKeepsPersonalDataOutOfLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	}))

	handler := NewHTTPLoggingMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, personalDataTarget, nil)
	req.Header.Set("Referer", personalDataReferer)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	logs := buf.String()
	require.NotEmpty(t, logs, "middleware emitted no log lines, so the assertions below would pass vacuously")
	// A URL reaches the log through url.URL.String(), which percent-encodes
	// the "@", so both forms of an address must be absent.
	for _, email := range []string{personalDataEmail, refererEmail} {
		require.NotContains(t, logs, email, "email reached the logs")
		require.NotContains(t, logs, url.QueryEscape(email), "percent-encoded email reached the logs")
	}
	require.NotContains(t, logs, "supersecret", "token reached the logs")
	require.Contains(t, logs, "REDACTED")
}

// The request context is the second sink: every downstream handler that logs
// inherits ReqURL from here, so a raw URL stored on it leaks far from this
// middleware.
func TestHTTPLoggingMiddlewareKeepsPersonalDataOutOfRequestContext(t *testing.T) {
	t.Parallel()

	var (
		captured *contextvalues.RequestContext
		found    bool
	)

	handler := NewHTTPLoggingMiddleware(testenv.NewLogger(t))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, found = contextvalues.GetRequestContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, personalDataTarget, nil)
	req.Header.Set("Referer", personalDataReferer)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, found, "middleware did not install a request context")
	require.Equal(t, "/rpc/auth.login?email=REDACTED&org_name=acme&token=REDACTED", captured.ReqURL)
	require.Equal(t, "https://app.example.com/signup?email=REDACTED", captured.Referer)
}

// A late error-path WriteHeader after the response has committed is ignored
// by net/http; the wrapper's recorded status must not change either, or
// access logs relabel an already-sent response (e.g. a relayed SSE stream
// that dies mid-flight gets logged as a 500 despite a 200 on the wire).
func TestResponseWriterIgnoresWriteHeaderAfterWriteHeader(t *testing.T) {
	t.Parallel()

	inner := httptest.NewRecorder()
	rw := newResponseWriter(inner)

	rw.WriteHeader(http.StatusOK)
	rw.WriteHeader(http.StatusInternalServerError)

	require.Equal(t, http.StatusOK, rw.statusCode)
	require.Equal(t, http.StatusOK, inner.Code)
}

func TestResponseWriterIgnoresWriteHeaderAfterWrite(t *testing.T) {
	t.Parallel()

	inner := httptest.NewRecorder()
	rw := newResponseWriter(inner)

	_, err := rw.Write([]byte("body"))
	require.NoError(t, err)
	rw.WriteHeader(http.StatusInternalServerError)

	require.Equal(t, http.StatusOK, rw.statusCode)
	require.Equal(t, http.StatusOK, inner.Code)
}

func TestResponseWriterIgnoresWriteHeaderAfterFlush(t *testing.T) {
	t.Parallel()

	inner := httptest.NewRecorder()
	rw := newResponseWriter(inner)

	rw.Flush()
	rw.WriteHeader(http.StatusInternalServerError)

	require.Equal(t, http.StatusOK, rw.statusCode)
}
