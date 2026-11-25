package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamHostCandidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "blank is no filter", in: "  ", want: []string{}},
		{name: "parent domains stop at two labels", in: "mcp.linear.app", want: []string{"mcp.linear.app", "linear.app"}},
		{name: "lowercased and trimmed", in: " MCP.Linear.App. ", want: []string{"mcp.linear.app", "linear.app"}},
		{name: "default port dropped", in: "mcp.linear.app:443", want: []string{"mcp.linear.app", "linear.app"}},
		{name: "other port kept on every candidate", in: "a.b.test:8443", want: []string{"a.b.test:8443", "b.test:8443"}},
		{name: "single label host", in: "localhost:3000", want: []string{"localhost:3000"}},
		{name: "ipv4 has no parents", in: "10.0.0.1", want: []string{"10.0.0.1"}},
		{name: "ipv6 with port", in: "[::1]:8080", want: []string{"[::1]:8080"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := upstreamHostCandidates(tt.in)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	for _, bad := range []string{"https://mcp.linear.app", "mcp.linear.app/mcp", "user@host.test", ":443"} {
		_, err := upstreamHostCandidates(bad)
		require.Error(t, err, bad)
	}
}

func TestContainsPattern(t *testing.T) {
	t.Parallel()

	require.False(t, containsPattern("   ").Valid)
	require.Equal(t, `%50\%\_off\\%`, containsPattern(` 50%_off\ `).String)
}
