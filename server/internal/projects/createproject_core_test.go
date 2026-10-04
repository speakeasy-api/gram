package projects_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/projects"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// The dashboard create and the Platform MCP create_project share one core.
// This pins what that core writes on the dashboard side — the same records
// the Platform MCP test asserts for an agent-created project — so extracting
// it cannot silently drop one of them.
func TestCreateProjectWritesDefaultEnvironmentAndDefaultPlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	pluginCreates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginCreate)
	require.NoError(t, err)

	suffix := uuid.NewString()[:8]
	result, err := ti.service.CreateProject(ctx, &gen.CreateProjectPayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "Side Records " + suffix,
	})
	require.NoError(t, err)
	require.Equal(t, "side-records-"+suffix, string(result.Project.Slug))
	projectID, err := uuid.Parse(result.Project.ID)
	require.NoError(t, err)

	environments, err := envrepo.New(ti.conn).ListEnvironments(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, environments, 1)
	require.Equal(t, "Default", environments[0].Name)
	require.Equal(t, "default", environments[0].Slug)
	require.Equal(t, "Default project for organization", environments[0].Description.String)

	_, err = pluginsrepo.New(ti.conn).GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID})
	require.NoError(t, err)
	afterPluginCreates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginCreate)
	require.NoError(t, err)
	require.Equal(t, pluginCreates+1, afterPluginCreates)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionProjectCreate)
	require.NoError(t, err)
	require.Equal(t, result.Project.ID, entry.SubjectID)
	require.Equal(t, authCtx.UserID, entry.ActorID)
}

// A name with no letter or digit derives an empty slug, which the projects
// table refuses. The core refuses it first, as an invalid request rather than
// a server error, on both surfaces.
func TestCreateProjectRefusesNameWithoutLettersOrDigits(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	before, err := projectsrepo.New(ti.conn).ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)

	_, err = ti.service.CreateProject(ctx, &gen.CreateProjectPayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "!!! ???",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.ErrorIs(t, err, projects.ErrProjectSlugEmpty)

	after, err := projectsrepo.New(ti.conn).ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, after, len(before))
}

func TestProjectSlugMatchesTheDashboardDerivation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		slug string
		err  error
	}{
		{name: "words", in: "Support Team", slug: "support-team"},
		{name: "punctuation dropped", in: "support team!", slug: "support-team"},
		{name: "no letters or digits", in: "!!! ???", err: projects.ErrProjectSlugEmpty},
		{name: "blank", in: "   ", err: projects.ErrProjectNameInvalid},
		{name: "too long", in: "abcdefghijabcdefghijabcdefghijabcdefghijk", err: projects.ErrProjectNameInvalid},
		{name: "nul byte", in: "support\x00team", err: projects.ErrProjectNameInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			slug, err := projects.ProjectSlug(tc.in)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.slug, slug)
		})
	}
}
