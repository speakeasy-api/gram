package protectedresource

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEndpointRejectsUnsafeURLsBeforeDiscovery(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{"http://example.test/mcp", "https://user:secret@example.test/mcp", "https://example.test/mcp#fragment", "/relative"} {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			// A nil policy would panic if discovery ran before input validation.
			_, err := DiscoverEndpointResource(context.Background(), nil, endpoint)
			require.ErrorIs(t, err, ErrEndpointResourceInvalid)
		})
	}
}

func TestHTTPSOriginDefaultPort(t *testing.T) {
	t.Parallel()
	origin, err := url.Parse("https://example.test")
	require.NoError(t, err)
	for _, tc := range []struct {
		candidate string
		same      bool
	}{
		{"https://EXAMPLE.test:443/mcp", true},
		{"https://example.test:444/mcp", false},
		{"http://example.test:443/mcp", false},
		{"https://other.example.test/mcp", false},
	} {
		candidate, err := url.Parse(tc.candidate)
		require.NoError(t, err)
		require.Equal(t, tc.same, sameHTTPSOrigin(origin, candidate))
	}
}
