package remotesessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestNormalizeVendorIssuer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want string
	}{
		{in: "https://mcp.linear.app", want: "https://mcp.linear.app"},
		{in: "https://mcp.linear.app/", want: "https://mcp.linear.app"},
		{in: "HTTPS://MCP.Linear.App/", want: "https://mcp.linear.app"},
		{in: "https://mcp.linear.app:443/", want: "https://mcp.linear.app"},
		{in: "https://auth.example.com/Tenant/", want: "https://auth.example.com/Tenant"},
		{in: "https://auth.example.com:8443/a", want: "https://auth.example.com:8443/a"},
	} {
		got, err := remotesessions.NormalizeVendorIssuer(tc.in)
		require.NoError(t, err, tc.in)
		require.Equal(t, tc.want, got, tc.in)
	}
	for _, bad := range []string{"", "mcp.linear.app", "ftp://example.com", "https://example.com/?a=b", "https://example.com/#f", "https://user@example.com"} {
		_, err := remotesessions.NormalizeVendorIssuer(bad)
		require.ErrorIs(t, err, remotesessions.ErrInvalidVendorIssuer, bad)
	}
}
