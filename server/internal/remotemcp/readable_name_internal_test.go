package remotemcp

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestRemoteMCPDisplayName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		url  string
		want string
	}{
		{url: "https://mcp.ramp.com/mcp", want: "Ramp"},
		{url: "https://MCP.Ramp.COM/mcp", want: "Ramp"},
		{url: "https://mcp.über.example/mcp", want: "Über"},
		{url: "https://mcp.canva.com./mcp", want: "Canva"},
		{url: "https://app.asana.com", want: "Asana"},
		{url: "https://mcp.acme-wiki.example/mcp", want: "Acme Wiki"},
		{url: "https://api.example.co.uk/mcp", want: "Example"},
		{url: "http://127.0.0.1:4100/mcp", want: "127.0.0.1"},
		{url: "https://localhost:8080/rpc/mcp", want: "localhost"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, remoteMCPDisplayName(tc.url, pgtype.Text{String: "", Valid: false}))
		})
	}
}

func TestRemoteMCPDisplayName_KeepsAGivenName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Team tools", remoteMCPDisplayName("https://mcp.ramp.com/mcp", pgtype.Text{String: "Team tools", Valid: true}))
}
