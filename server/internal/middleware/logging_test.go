package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

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
			name: "shared handoff path segment redacted",
			in:   "/shared/handoffs/aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
			want: "/shared/handoffs/REDACTED",
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
