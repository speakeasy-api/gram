package workloadpolicy_test

import (
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDashboardProject_SwitchIncidentalBinding(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	first, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, ti.projectID)
	require.NoError(t, err)
	second, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{OrganizationID: ti.orgID, Name: "Second project", Slug: "second-project"})
	require.NoError(t, err)
	for _, project := range []struct {
		slug string
		id   string
	}{{second.Slug, second.ID.String()}, {first.Slug, first.ID.String()}} {
		policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{Name: "Switch project", Issuer: githubIssuer, JwksURI: githubJWKS, ProjectScoped: true, ProjectSlugInput: &project.slug})
		require.NoError(t, err)
		require.Equal(t, project.id, onlyIssuer(t, policy).ProjectID)
	}
	require.Equal(t, ti.projectID, *ac.ProjectID, "selection must not mutate caller context")
	_, err = ti.service.List(asAPIKey(t, ctx), &gen.ListPayload{ProjectSlugInput: &second.Slug})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.List(asAPIKey(t, ctx), &gen.ListPayload{ProjectSlugInput: &first.Slug})
	require.NoError(t, err)
}

func TestDashboardProject_ForeignIncidentalBindingCannotSelectForeignProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	foreignCtx, foreign := newTestService(t)
	project, err := projectsrepo.New(foreign.conn).CreateProject(foreignCtx, projectsrepo.CreateProjectParams{OrganizationID: foreign.orgID, Name: "Foreign project", Slug: "foreign-switch-project"})
	require.NoError(t, err)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	cloned := *ac
	cloned.ProjectID = &project.ID
	cloned.ProjectSlug = &project.Slug
	ctx = contextvalues.SetAuthContext(ctx, &cloned)
	_, err = ti.service.List(ctx, &gen.ListPayload{ProjectSlugInput: &project.Slug})
	requireOopsCode(t, err, oops.CodeForbidden)
}
