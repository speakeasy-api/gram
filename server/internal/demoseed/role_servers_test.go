//go:build demoseed_safety

package demoseed

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	pluginrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestDemoRoleServersSurviveReseed(t *testing.T) {
	t.Parallel()
	testRoleServersSurviveReseed(t, DefaultSpec())
}

func TestLocalRoleServersSurviveReseed(t *testing.T) {
	t.Parallel()
	testRoleServersSurviveReseed(t, LocalSpec())
}

func testRoleServersSurviveReseed(t *testing.T, spec Spec) {
	t.Helper()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	q := pluginrepo.New(db)
	projectID := uuid.MustParse(spec.ProjectID())
	var firstMemberships map[string][]string

	for iteration := range 2 {
		seedLocalPostgres(ctx, t, db, spec)
		plugins, err := q.ListActivePluginsForProject(ctx, pluginrepo.ListActivePluginsForProjectParams{ProjectID: projectID})
		require.NoError(t, err)
		memberships := map[string][]string{}
		for _, plugin := range plugins {
			// Seeded role plugins must remain role-only, beyond the content samples below.
			if plugin.AutoCreated && !plugin.IsDefault.Bool {
				assignments, err := q.ListPluginAssignments(ctx, pluginrepo.ListPluginAssignmentsParams{
					PluginID: plugin.ID, OrganizationID: spec.OrgID, ProjectID: projectID,
				})
				require.NoError(t, err)
				require.NotEmpty(t, assignments)
				for _, assignment := range assignments {
					require.Regexp(t, `^role:`, assignment.PrincipalUrn, "role content must not widen audiences to Everyone")
				}
			}
			if plugin.Slug != "engineer" && plugin.Slug != "read-only-tools" {
				continue
			}
			servers, err := q.ListPluginServers(ctx, plugin.ID)
			require.NoError(t, err)
			memberships[plugin.Slug] = []string{}
			for _, server := range servers {
				require.True(t, server.McpServerID.Valid)
				require.False(t, server.ToolsetID.Valid)
				require.False(t, server.MetaMcpServerID.Valid)
				backend, err := q.GetMcpServerForPluginServer(ctx, pluginrepo.GetMcpServerForPluginServerParams{
					McpServerID: server.McpServerID.UUID, ProjectID: projectID,
				})
				require.NoError(t, err)
				require.True(t, backend.Slug.Valid)
				require.NotEmpty(t, backend.Slug.String)
				// Seed row IDs may change; backend slugs identify the delivered content.
				memberships[plugin.Slug] = append(memberships[plugin.Slug], backend.Slug.String)
			}
			slices.Sort(memberships[plugin.Slug])

		}
		// Representative build and read-only audiences must receive usable content.
		for _, slug := range []string{"engineer", "read-only-tools"} {
			require.NotEmpty(t, memberships[slug], "role plugin %s must deliver servers", slug)
		}
		if iteration == 0 {
			firstMemberships = memberships
		} else {
			require.Equal(t, firstMemberships, memberships, "reseed must preserve role backend memberships")
		}
	}
}
