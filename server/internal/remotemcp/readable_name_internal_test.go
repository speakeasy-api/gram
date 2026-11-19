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
		{url: "https://mcp.canva.com./mcp", want: "Canva"},
		{url: "https://app.asana.com", want: "Asana"},
		{url: "https://mcp.acme-wiki.example/mcp", want: "Acme Wiki"},
		{url: "https://api.example.co.uk/mcp", want: "Example"},
		{url: "https://api.example.or.jp/mcp", want: "Example"},
		{url: "https://api.example.edu.au/mcp", want: "Example"},
		{url: "https://mcp.example.me.uk/mcp", want: "Example"},
		{url: "https://mcp.foo-xn--bar.example/mcp", want: "Foo Xn Bar"},
		// Hosts that do not name a service keep the address as the name.
		{url: "http://127.0.0.1:4100/mcp", want: "127.0.0.1:4100/mcp"},
		{url: "http://[::1]:4100/mcp", want: "[::1]:4100/mcp"},
		{url: "https://localhost:8080/rpc/mcp", want: "localhost:8080/rpc/mcp"},
		{url: "https://foo.vercel.app/mcp", want: "foo.vercel.app/mcp"},
		{url: "https://svc-x.a.run.app/mcp", want: "svc-x.a.run.app/mcp"},
		{url: "https://mcp.über.example/mcp", want: "mcp.über.example/mcp"},
		{url: "https://mcp.xn--ber-goa.example/mcp", want: "mcp.xn--ber-goa.example/mcp"},
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
