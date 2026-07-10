package thirdparty

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenRouterRateLimitIsAccountWide(t *testing.T) {
	t.Parallel()

	policy := OpenRouterHTTPRateLimit()
	first, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", nil)
	require.NoError(t, err)
	first.Header.Set("Authorization", "Bearer org-one-key")
	second, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://openrouter.ai/api/v1/key", nil)
	require.NoError(t, err)
	second.Header.Set("Authorization", "Bearer provisioning-key")

	require.Equal(t, "account", policy.KeyFor(first))
	require.Equal(t, policy.KeyFor(first), policy.KeyFor(second))
	require.Equal(t, 250, policy.Rate.Tokens)
	require.Equal(t, time.Minute, policy.Rate.Interval)
	require.Equal(t, 50, policy.Rate.Burst)
}

func TestFixedScopeVendorPolicies(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)
	require.Equal(t, "global", WorkOSHTTPRateLimit().KeyFor(req))
	require.Equal(t, "team", LoopsHTTPRateLimit().KeyFor(req))
}
