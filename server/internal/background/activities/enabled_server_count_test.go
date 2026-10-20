package activities_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// The enabled-server count reads hosted servers from their canonical wrapper but
// must keep counting exactly the toolsets the toolset flag counted before.
func TestEnabledServerCountMatchesToolsetFlagCount(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "enabled_server_count")
	require.NoError(t, err)

	orgID := createCountOrg(t, db)
	project := createCountProject(t, db, orgID)

	// Counted: wrapped private, wrapped public, unwrapped legacy, wrapper deleted.
	private := createCountToolset(t, db, orgID, project.ID, "wrapped-private", true, false)
	createWrapper(t, db, project.ID, private.ID, private.ID, visibility.Private)
	public := createCountToolset(t, db, orgID, project.ID, "wrapped-public", true, true)
	createWrapper(t, db, project.ID, public.ID, public.ID, visibility.Public)
	createCountToolset(t, db, orgID, project.ID, "legacy", true, false)
	unwrapped := createCountToolset(t, db, orgID, project.ID, "wrapper-deleted", true, false)
	createWrapper(t, db, project.ID, unwrapped.ID, unwrapped.ID, visibility.Private)
	_, err = mcpserversrepo.New(db).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: unwrapped.ID, ProjectID: project.ID})
	require.NoError(t, err)

	// Not counted, as before: a fresh-id member wrapper, a disabled server, deleted toolsets, a remote server.
	createWrapper(t, db, project.ID, uuid.New(), private.ID, visibility.Private)
	disabled := createCountToolset(t, db, orgID, project.ID, "disabled", false, true)
	createWrapper(t, db, project.ID, disabled.ID, disabled.ID, visibility.Disabled)
	deleted := createCountToolset(t, db, orgID, project.ID, "deleted", true, false)
	createWrapper(t, db, project.ID, deleted.ID, deleted.ID, visibility.Private)
	_, err = mcpserversrepo.New(db).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: deleted.ID, ProjectID: project.ID})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(db).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: deleted.Slug, ProjectID: project.ID})
	require.NoError(t, err)
	orphaned := createCountToolset(t, db, orgID, project.ID, "orphaned", true, false)
	createWrapper(t, db, project.ID, orphaned.ID, orphaned.ID, visibility.Private)
	_, err = toolsetsrepo.New(db).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: orphaned.Slug, ProjectID: project.ID})
	require.NoError(t, err)
	createRemoteServer(t, db, project.ID)

	// A deleted project's live toolsets were counted before and still are.
	gone := createCountProject(t, db, orgID)
	inGone := createCountToolset(t, db, orgID, gone.ID, "in-deleted-project", true, false)
	createWrapper(t, db, gone.ID, inGone.ID, inGone.ID, visibility.Private)
	_, err = projectsrepo.New(db).DeleteProject(ctx, gone.ID)
	require.NoError(t, err)

	const want = 5
	count, err := usagerepo.New(db).GetEnabledServerCount(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, int64(want), count)

	metrics := collectOrgMetrics(t, db, orgID)
	require.Equal(t, int64(want), metrics.TotalEnabledServers)
	require.Equal(t, int64(2), metrics.PublicMCPServers)
	require.Equal(t, int64(4), metrics.PrivateMCPServers)
}

// A wrapped toolset counts by its wrapper's visibility, not its own flag, in both directions.
func TestEnabledServerCountReadsWrapperVisibility(t *testing.T) {
	t.Parallel()

	db, err := infra.CloneTestDatabase(t, "enabled_server_count_wrapper")
	require.NoError(t, err)

	cases := []struct {
		name      string
		flag      bool
		vis       string
		otherProj bool
		wantCount int64
	}{
		{name: "flag-enabled-wrapper-disabled", flag: true, vis: visibility.Disabled, otherProj: false, wantCount: 0},
		{name: "flag-disabled-wrapper-private", flag: false, vis: visibility.Private, otherProj: false, wantCount: 1},
		// A canonical-id wrapper in another project is not this toolset's wrapper.
		{name: "wrapper-in-other-project", flag: false, vis: visibility.Private, otherProj: true, wantCount: 0},
	}
	for _, tc := range cases {
		orgID := createCountOrg(t, db)
		project := createCountProject(t, db, orgID)
		toolset := createCountToolset(t, db, orgID, project.ID, tc.name, tc.flag, false)
		wrapperProject := project.ID
		if tc.otherProj {
			wrapperProject = createCountProject(t, db, orgID).ID
		}
		createWrapper(t, db, wrapperProject, toolset.ID, toolset.ID, tc.vis)
		createRemoteServer(t, db, project.ID)

		count, err := usagerepo.New(db).GetEnabledServerCount(t.Context(), orgID)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.wantCount, count, tc.name)
		require.Equal(t, tc.wantCount, collectOrgMetrics(t, db, orgID).TotalEnabledServers, tc.name)
	}
}

func collectOrgMetrics(t *testing.T, db *pgxpool.Pool, orgID string) activities.PlatformUsageMetrics {
	t.Helper()

	all, err := activities.NewCollectPlatformUsageMetrics(testenv.NewLogger(t), db).Do(t.Context())
	require.NoError(t, err)
	for _, m := range all {
		if m.OrganizationID == orgID {
			return m
		}
	}
	require.FailNow(t, "organization missing from platform usage metrics")
	return activities.PlatformUsageMetrics{}
}

func createCountOrg(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()

	orgID := "org-" + uuid.NewString()[:8]
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Count Org", Slug: orgID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	return orgID
}

func createCountProject(t *testing.T, db *pgxpool.Pool, orgID string) projectsrepo.Project {
	t.Helper()

	project, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Count Project", Slug: "count-" + uuid.NewString()[:8], OrganizationID: orgID,
	})
	require.NoError(t, err)
	return project
}

func createCountToolset(t *testing.T, db *pgxpool.Pool, orgID string, projectID uuid.UUID, slug string, enabled, public bool) toolsetsrepo.Toolset {
	t.Helper()

	q := toolsetsrepo.New(db)
	mcpSlug := pgtype.Text{String: slug + "-" + uuid.NewString()[:8], Valid: true}
	toolset, err := q.CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{
		OrganizationID: orgID, ProjectID: projectID, Name: slug, Slug: slug,
		Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{}, McpSlug: mcpSlug, McpEnabled: enabled,
	})
	require.NoError(t, err)
	require.NoError(t, q.SetToolsetMCPPublicByID(t.Context(), toolsetsrepo.SetToolsetMCPPublicByIDParams{McpIsPublic: public, ID: toolset.ID, ProjectID: projectID}))
	return toolset
}

func createWrapper(t *testing.T, db *pgxpool.Pool, projectID, id, toolsetID uuid.UUID, vis string) {
	t.Helper()

	_, err := mcpserversrepo.New(db).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
		ID: id, ProjectID: projectID, Name: pgtype.Text{}, Slug: pgtype.Text{},
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: uuid.NullUUID{}, RemoteMcpServerID: uuid.NullUUID{},
		TunneledMcpServerID: uuid.NullUUID{}, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true},
		UnproxiedMcpServerID: uuid.NullUUID{}, ToolVariationsGroupID: uuid.NullUUID{}, Visibility: vis, NetworkAccessMode: pgtype.Text{},
	})
	require.NoError(t, err)
}

func createRemoteServer(t *testing.T, db *pgxpool.Pool, projectID uuid.UUID) {
	t.Helper()

	remote, err := remotemcprepo.New(db).CreateServer(t.Context(), remotemcprepo.CreateServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: pgtype.Text{}, Slug: pgtype.Text{},
		TransportType: string(externalmcptypes.TransportTypeStreamableHTTP), Url: "https://remote.example.test/mcp",
	})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(db).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: pgtype.Text{}, Slug: pgtype.Text{},
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: uuid.NullUUID{}, RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true},
		TunneledMcpServerID: uuid.NullUUID{}, ToolsetID: uuid.NullUUID{}, UnproxiedMcpServerID: uuid.NullUUID{},
		ToolVariationsGroupID: uuid.NullUUID{}, Visibility: visibility.Public, NetworkAccessMode: pgtype.Text{},
	})
	require.NoError(t, err)
}
