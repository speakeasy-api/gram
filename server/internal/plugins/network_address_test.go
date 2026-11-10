package plugins

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestRemoteServerURL(t *testing.T) {
	t.Parallel()
	vendor := pgtype.Text{String: "https://vendor.example/mcp", Valid: true}
	none := pgtype.Text{String: "", Valid: false}
	for _, tc := range []struct {
		name          string
		row           pluginMCPServerRow
		want          string
		wantUnproxied bool
		wantError     bool
	}{
		{name: "endpoint on the server URL", row: pluginRow(repo.ListPluginsWithMcpServersForProjectRow{EndpointSlug: "remote"}, none, none), want: "https://public.example/mcp/remote"},
		{name: "custom domain endpoint wins", row: pluginRow(repo.ListPluginsWithMcpServersForProjectRow{EndpointSlug: "remote", EndpointCustomDomain: pgtype.Text{String: "mcp.acme.example", Valid: true}}, none, none), want: "https://mcp.acme.example/mcp/remote"},
		{name: "unproxied URL is the vendor's", row: pluginRow(repo.ListPluginsWithMcpServersForProjectRow{EndpointSlug: "remote", UnproxiedUrl: vendor}, none, none), want: "https://vendor.example/mcp", wantUnproxied: true},
		{name: "unproxied server cannot be private", row: pluginRow(repo.ListPluginsWithMcpServersForProjectRow{UnproxiedUrl: vendor, NetworkAccessMode: pgtype.Text{String: "private_only", Valid: true}}, none, none), wantUnproxied: true, wantError: true},
		{name: "server address overrides the endpoint", row: pluginRow(repo.ListPluginsWithMcpServersForProjectRow{EndpointSlug: "remote", EndpointCustomDomain: pgtype.Text{String: "old.example", Valid: true}}, pgtype.Text{String: "renamed", Valid: true}, pgtype.Text{String: "mcp.acme.example", Valid: true}), want: "https://mcp.acme.example/mcp/renamed"},
		{name: "no endpoint fails closed", row: pluginRow(repo.ListPluginsWithMcpServersForProjectRow{}, none, none), wantError: true},
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

func pluginRow(row repo.ListPluginsWithMcpServersForProjectRow, slug, domain pgtype.Text) pluginMCPServerRow {
	return pluginMCPServerRow{ListPluginsWithMcpServersForProjectRow: row, AddressSlug: slug, AddressCustomDomain: domain}
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
