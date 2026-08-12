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
