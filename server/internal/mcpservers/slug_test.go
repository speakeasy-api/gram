package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers"
)

func TestDashboardRouteParam(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("7f1c2a9e-1111-4c4c-9a9a-000000000001")
	tests := []struct {
		name string
		slug string
		want string
	}{
		{name: "slug", slug: "my-server-0001", want: "my-server-0001"},
		{name: "trimmed slug", slug: " my-server-0001 ", want: "my-server-0001"},
		{name: "empty", slug: "", want: id.String()},
		{name: "add", slug: "add", want: id.String()},
		{name: "catalog", slug: "catalog", want: id.String()},
		{name: "deployments", slug: "deployments", want: id.String()},
		{name: "sources", slug: "sources", want: id.String()},
		{name: "organization-gateway", slug: "organization-gateway", want: id.String()},
		{name: "gateway", slug: "gateway", want: id.String()},
		{name: "x", slug: "x", want: id.String()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, mcpservers.DashboardRouteParam(tt.slug, id))
		})
	}
}
