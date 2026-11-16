package mcproute_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   mcproute.Base
		want mcproute.Base
		ok   bool
	}{
		{in: "", want: mcproute.MCP, ok: true},
		{in: mcproute.MCP, want: mcproute.MCP, ok: true},
		{in: mcproute.OrganizationGateway, want: mcproute.OrganizationGateway, ok: true},
		{in: "x/mcp", want: "", ok: false},
		{in: "/mcp", want: "", ok: false},
	} {
		got, ok := tc.in.Resolve()
		require.Equal(t, tc.ok, ok, tc.in)
		require.Equal(t, tc.want, got, tc.in)
	}
}
