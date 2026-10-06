package plugins

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

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
