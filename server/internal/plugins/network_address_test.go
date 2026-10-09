package plugins

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestToolsetServerURL(t *testing.T) {
	t.Parallel()
	slug := pgtype.Text{String: "tools", Valid: true}
	for _, tc := range []struct {
		name      string
		row       repo.ListPluginsWithServersForProjectRow
		want      string
		wantOK    bool
		wantError bool
	}{
		{name: "no MCP slug is not packaged", row: repo.ListPluginsWithServersForProjectRow{}, wantOK: false},
		{name: "unwrapped toolset uses the server URL", row: repo.ListPluginsWithServersForProjectRow{ToolsetMcpSlug: slug}, want: "https://public.example/mcp/tools", wantOK: true},
		{name: "custom domain wins", row: repo.ListPluginsWithServersForProjectRow{ToolsetMcpSlug: slug, ToolsetCustomDomain: pgtype.Text{String: "mcp.acme.example", Valid: true}}, want: "https://mcp.acme.example/mcp/tools", wantOK: true},
		{name: "wrapper picks its network namespace", row: repo.ListPluginsWithServersForProjectRow{ToolsetMcpSlug: slug, WrapperCount: 1, WrapperNetworkAccessMode: pgtype.Text{String: "private_only", Valid: true}, PrivateDnsName: pgtype.Text{String: "tail.example", Valid: true}, PrivateEndpointSlug: "private"}, want: "https://tail.example/mcp/private", wantOK: true},
		{name: "several wrappers are ambiguous", row: repo.ListPluginsWithServersForProjectRow{ToolsetMcpSlug: slug, WrapperCount: 2}, wantOK: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok, err := toolsetServerURL("https://public.example", tc.row)
			require.Equal(t, tc.wantOK, ok)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestRemoteServerURL(t *testing.T) {
	t.Parallel()
	vendor := pgtype.Text{String: "https://vendor.example/mcp", Valid: true}
	for _, tc := range []struct {
		name          string
		row           repo.ListPluginsWithMcpServersForProjectRow
		want          string
		wantUnproxied bool
		wantError     bool
	}{
		{name: "endpoint on the server URL", row: repo.ListPluginsWithMcpServersForProjectRow{EndpointSlug: "remote"}, want: "https://public.example/mcp/remote"},
		{name: "custom domain endpoint wins", row: repo.ListPluginsWithMcpServersForProjectRow{EndpointSlug: "remote", EndpointCustomDomain: pgtype.Text{String: "mcp.acme.example", Valid: true}}, want: "https://mcp.acme.example/mcp/remote"},
		{name: "unproxied URL is the vendor's", row: repo.ListPluginsWithMcpServersForProjectRow{EndpointSlug: "remote", UnproxiedUrl: vendor}, want: "https://vendor.example/mcp", wantUnproxied: true},
		{name: "unproxied server cannot be private", row: repo.ListPluginsWithMcpServersForProjectRow{UnproxiedUrl: vendor, NetworkAccessMode: pgtype.Text{String: "private_only", Valid: true}}, wantUnproxied: true, wantError: true},
		{name: "no endpoint fails closed", row: repo.ListPluginsWithMcpServersForProjectRow{}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, unproxied, err := remoteServerURL("https://public.example", tc.row)
			require.Equal(t, tc.wantUnproxied, unproxied)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPackageMCPURL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		mode        pgtype.Text
		publicSlug  string
		privateDNS  string
		privateSlug string
		want        string
		wantError   bool
	}{
		{name: "legacy public", publicSlug: "tools", want: "https://public.example/mcp/tools"},
		{name: "dual prefers public", mode: pgtype.Text{String: "dual", Valid: true}, publicSlug: "tools", privateDNS: "tail.example", privateSlug: "private", want: "https://public.example/mcp/tools"},
		{name: "private selects pinned namespace", mode: pgtype.Text{String: "private_only", Valid: true}, publicSlug: "tools", privateDNS: "tail.example", privateSlug: "private", want: "https://tail.example/mcp/private"},
		{name: "missing private endpoint fails closed", mode: pgtype.Text{String: "private_only", Valid: true}, publicSlug: "tools", privateDNS: "tail.example", wantError: true},
		{name: "missing ingress DNS fails closed", mode: pgtype.Text{String: "private_only", Valid: true}, publicSlug: "tools", privateSlug: "private", wantError: true},
		{name: "unknown mode fails closed", mode: pgtype.Text{String: "future", Valid: true}, publicSlug: "tools", wantError: true},
		{name: "no public endpoint fails closed", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := packageMCPURL(tc.mode, "https://public.example", tc.publicSlug, tc.privateDNS, tc.privateSlug)
			if tc.wantError {
				require.Error(t, err)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
