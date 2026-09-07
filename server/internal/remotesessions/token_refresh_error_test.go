package remotesessions

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsTokenRefreshRateLimited(t *testing.T) {
	t.Parallel()

	rateLimited := newTokenRefreshErrorFromHTTP(
		http.StatusTooManyRequests,
		"429 Too Many Requests",
		[]byte(`{"error":"temporarily_unavailable"}`),
	)
	require.True(t, IsTokenRefreshRateLimited(fmt.Errorf("refresh failed: %w", rateLimited)))

	notRateLimited := newTokenRefreshErrorFromHTTP(
		http.StatusBadRequest,
		"400 Bad Request",
		[]byte(`{"error":"invalid_grant"}`),
	)
	require.False(t, IsTokenRefreshRateLimited(notRateLimited))
	require.False(t, IsTokenRefreshRateLimited(fmt.Errorf("network unavailable")))
}

func TestNewTokenRefreshErrorFromHTTP_RFC6749Body(t *testing.T) {
	t.Parallel()

	err := newTokenRefreshErrorFromHTTP(
		http.StatusBadRequest,
		"400 Bad Request",
		[]byte(`{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}`),
	)
	require.True(t, err.invalidGrant())
	require.Equal(t, "invalid_grant: Unknown or invalid refresh token.", err.Reason)
	require.ErrorContains(t, err, "refresh endpoint 400 Bad Request")
}

// A body without a recognizable error falls back to the HTTP status, which is
// also the diagnostic tell in logs that an upstream body was not parsed.
func TestNewTokenRefreshErrorFromHTTP_StatusFallback(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		``,
		`<html>Bad Gateway</html>`,
		`{"errors":["Bad Request"]}`,
		`{"error":{"message":"no code"}}`,
	} {
		err := newTokenRefreshErrorFromHTTP(http.StatusBadGateway, "502 Bad Gateway", []byte(body))
		require.False(t, err.invalidGrant(), body)
		require.Equal(t, "HTTP 502 Bad Gateway", err.Reason, body)
	}
}
