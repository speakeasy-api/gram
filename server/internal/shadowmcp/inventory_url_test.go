package shadowmcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestCanonicalizeInventoryURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		raw    string
		want   shadowmcp.InventoryURL
		wantOK bool
	}{
		{
			name: "strips query and fragment",
			raw:  "HTTPS://MCP.Speakeasy.COM:443/mcp?token=secret#frag",
			want: shadowmcp.InventoryURL{
				CanonicalURL: "https://mcp.speakeasy.com/mcp",
				URLHost:      "mcp.speakeasy.com",
			},
			wantOK: true,
		},
		{
			name: "sanitizes embedded credentials",
			raw:  "https://user:pass@MCP.Speakeasy.COM:443/mcp?token=secret#frag",
			want: shadowmcp.InventoryURL{
				CanonicalURL: "https://mcp.speakeasy.com/mcp",
				URLHost:      "mcp.speakeasy.com",
			},
			wantOK: true,
		},
		{
			name: "strips empty force query",
			raw:  "https://mcp.speakeasy.com/mcp?",
			want: shadowmcp.InventoryURL{
				CanonicalURL: "https://mcp.speakeasy.com/mcp",
				URLHost:      "mcp.speakeasy.com",
			},
			wantOK: true,
		},
		{
			name: "strips default port from ipv6 host",
			raw:  "https://[2001:db8::1]:443/mcp",
			want: shadowmcp.InventoryURL{
				CanonicalURL: "https://[2001:db8::1]/mcp",
				URLHost:      "2001:db8::1",
			},
			wantOK: true,
		},
		{
			name:   "rejects stdio command",
			raw:    "node ./server.js",
			wantOK: false,
		},
		{
			name:   "rejects url without host",
			raw:    "https:///mcp",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := shadowmcp.CanonicalizeInventoryURL(tt.raw)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestAccessEvidenceForInventoryURL(t *testing.T) {
	t.Parallel()

	evidence := shadowmcp.AccessEvidenceForInventoryURL(shadowmcp.InventoryURL{
		CanonicalURL: "https://mcp.speakeasy.com/mcp",
		URLHost:      "mcp.speakeasy.com",
	})

	require.Equal(t, "https://mcp.speakeasy.com/mcp", evidence.FullURL)
	require.Equal(t, "mcp.speakeasy.com", evidence.URLHost)
	require.Empty(t, evidence.ServerIdentity)
}
