package admin

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestGetOrganization_WorkOSDashboardURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		environmentID string
		workosID      *string
		want          *string
	}{
		{
			name:          "configured and linked",
			environmentID: "environment_01EXAMPLE",
			workosID:      new("org_01EXAMPLE"),
			want:          new("https://dashboard.workos.com/environment_01EXAMPLE/organizations/org_01EXAMPLE"),
		},
		{
			name:          "configured but not linked",
			environmentID: "environment_01EXAMPLE",
			workosID:      nil,
			want:          nil,
		},
		{
			name:          "linked but not configured",
			environmentID: "",
			workosID:      new("org_01EXAMPLE"),
			want:          nil,
		},
		{
			name:          "blank configuration",
			environmentID: "   ",
			workosID:      new("org_01EXAMPLE"),
			want:          nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, svc, conn := newTestAdminService(t)
			svc.SetWorkOSEnvironmentID(tc.environmentID)
			seedOrg(t, ctx, conn, orgFixture{id: "org_workos_link", name: "Link Co", slug: "link-co", workosID: tc.workosID})

			org, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_workos_link"})
			require.NoError(t, err)
			require.Equal(t, tc.want, org.WorkosDashboardURL)
		})
	}
}

func TestListOrganizations_WorkOSDashboardURL(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	svc.SetWorkOSEnvironmentID("environment_01EXAMPLE")
	seedOrg(t, ctx, conn, orgFixture{id: "org_workos_list", name: "List Link Co", slug: "list-link-co", workosID: new("org_01LIST")})

	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Q: new("org_workos_list")})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 1)
	require.Equal(t, "https://dashboard.workos.com/environment_01EXAMPLE/organizations/org_01LIST", *res.Organizations[0].WorkosDashboardURL)
}

func TestWorkOSDashboardURL_EscapesPathSegments(t *testing.T) {
	t.Parallel()

	svc := &Service{workosEnvironmentID: "env/../x"}
	link := svc.workosDashboardURL(conv.ToPGText("org/1?a"))
	require.NotNil(t, link)
	require.Equal(t, "https://dashboard.workos.com/env%2F..%2Fx/organizations/org%2F1%3Fa", *link)
}
