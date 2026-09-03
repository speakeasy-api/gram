package dataexports_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestListForOrgReturnsExportsAcrossLiveProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	firstDestination := createDestination(t, ctx, ti, "https://first.example.test", "exclude")
	firstRoute, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		DataSource:        "product_telemetry",
		Enabled:           true,
		OtelDestinationID: &firstDestination.ID,
	})
	require.NoError(t, err)

	otherSlug := "data-exports-other-" + uuid.NewString()[:8]
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           otherSlug,
		Slug:           otherSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	otherAuthCtx := *authCtx
	otherAuthCtx.ProjectID = &otherProject.ID
	otherAuthCtx.ProjectSlug = &otherSlug
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuthCtx)

	secondDestination := createDestination(t, otherCtx, ti, "https://second.example.test", "include")
	secondRoute, err := ti.service.CreateRoute(otherCtx, &gen.CreateRoutePayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		DataSource:        "product_telemetry",
		Enabled:           true,
		OtelDestinationID: &secondDestination.ID,
	})
	require.NoError(t, err)

	orgAuthCtx := *authCtx
	orgAuthCtx.ProjectID = nil
	orgAuthCtx.ProjectSlug = nil
	orgCtx := contextvalues.SetAuthContext(ctx, &orgAuthCtx)
	result, err := ti.service.ListForOrg(orgCtx, &gen.ListForOrgPayload{SessionToken: nil})
	require.NoError(t, err)
	require.ElementsMatch(t, []*gen.Destination{firstDestination, secondDestination}, result.Destinations)
	require.ElementsMatch(t, []*gen.DataExportRoute{firstRoute, secondRoute}, result.Routes)
}
