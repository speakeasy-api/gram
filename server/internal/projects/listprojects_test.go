package projects_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestProjectsService_ListProjects_FiltersByBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	allowedProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "allowed-" + uuid.NewString()[:8],
		Slug:           "allowed-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	blockedProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "blocked-" + uuid.NewString()[:8],
		Slug:           "blocked-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	ctx = withAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildRead, Resource: allowedProject.ID.String()})

	result, err := ti.service.ListProjects(ctx, &gen.ListProjectsPayload{
		SessionToken:   nil,
		ApikeyToken:    nil,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	projectIDs := make([]string, 0, len(result.Projects))
	for _, project := range result.Projects {
		projectIDs = append(projectIDs, project.ID)
	}

	require.Contains(t, projectIDs, allowedProject.ID.String())
	require.NotContains(t, projectIDs, blockedProject.ID.String())
}
