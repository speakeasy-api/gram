package platformmcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeRemoteURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
		// invalid asserts the raw value is refused with ErrRemoteURLInvalid.
		invalid bool
	}{
		{
			name: "already normalized url is unchanged",
			raw:  "https://example.com/mcp",
			want: "https://example.com/mcp",
		},
		{
			name: "host is lowercased and path case is preserved",
			raw:  "https://EXAMPLE.com/MCP",
			want: "https://example.com/MCP",
		},
		{
			name: "uppercase scheme is lowercased",
			raw:  "HTTPS://example.com/mcp",
			want: "https://example.com/mcp",
		},
		{
			name: "default https port is stripped",
			raw:  "https://example.com:443/mcp",
			want: "https://example.com/mcp",
		},
		{
			name: "non-default port is preserved",
			raw:  "https://example.com:8443/mcp",
			want: "https://example.com:8443/mcp",
		},
		{
			name: "port merely ending in 443 is preserved",
			raw:  "https://example.com:1443/mcp",
			want: "https://example.com:1443/mcp",
		},
		{
			name: "dangling port separator is stripped",
			raw:  "https://example.com:/mcp",
			want: "https://example.com/mcp",
		},
		{
			name: "surrounding whitespace is trimmed",
			raw:  "  https://example.com/mcp\n",
			want: "https://example.com/mcp",
		},
		{
			name: "bare host without path is preserved",
			raw:  "https://example.com",
			want: "https://example.com",
		},
		{
			name: "query is preserved",
			raw:  "https://example.com/mcp?tenant=acme&mode=streamable",
			want: "https://example.com/mcp?tenant=acme&mode=streamable",
		},
		{
			name: "ipv6 literal keeps brackets when the default port is stripped",
			raw:  "https://[2001:DB8::1]:443/mcp",
			want: "https://[2001:db8::1]/mcp",
		},
		{
			name: "ipv6 literal without port is preserved",
			raw:  "https://[2001:db8::1]/mcp",
			want: "https://[2001:db8::1]/mcp",
		},
		{
			name:    "empty input",
			raw:     "",
			invalid: true,
		},
		{
			name:    "whitespace-only input",
			raw:     "   ",
			invalid: true,
		},
		{
			name:    "http scheme",
			raw:     "http://example.com/mcp",
			invalid: true,
		},
		{
			name:    "websocket scheme",
			raw:     "wss://example.com/mcp",
			invalid: true,
		},
		{
			name:    "missing scheme",
			raw:     "example.com/mcp",
			invalid: true,
		},
		{
			name:    "scheme-relative url",
			raw:     "//example.com/mcp",
			invalid: true,
		},
		{
			name:    "empty host",
			raw:     "https:///mcp",
			invalid: true,
		},
		{
			name:    "opaque url without authority",
			raw:     "https:example.com",
			invalid: true,
		},
		{
			name:    "userinfo",
			raw:     "https://user@example.com/mcp",
			invalid: true,
		},
		{
			name:    "userinfo with password",
			raw:     "https://user:secret@example.com/mcp",
			invalid: true,
		},
		{
			name:    "fragment",
			raw:     "https://example.com/mcp#section",
			invalid: true,
		},
		{
			name:    "bare fragment delimiter",
			raw:     "https://example.com/mcp#",
			invalid: true,
		},
		{
			name:    "unresolved template placeholder",
			raw:     "https://example.com/{tenant}/mcp",
			invalid: true,
		},
		{
			name:    "invalid port",
			raw:     "https://example.com:abc/mcp",
			invalid: true,
		},
		{
			name:    "overlong url",
			raw:     "https://example.com/" + strings.Repeat("a", maxRemoteURLLength),
			invalid: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeRemoteURL(test.raw)
			if test.invalid {
				require.ErrorIs(t, err, ErrRemoteURLInvalid)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

// Normalization is idempotent: the receipt codec re-normalizes at mint time
// and refuses any drift, so a normalized URL must round-trip to itself.
func TestNormalizeRemoteURLIsIdempotent(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"https://EXAMPLE.com:443/MCP?x=1",
		"https://[2001:DB8::1]:443/mcp",
		"  https://example.com:/mcp ",
	} {
		once, err := normalizeRemoteURL(raw)
		require.NoError(t, err)
		twice, err := normalizeRemoteURL(once)
		require.NoError(t, err)
		require.Equal(t, once, twice)
	}
}
