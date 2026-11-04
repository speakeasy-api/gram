package builtinmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	mcprepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestEnsureServer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	container, clone, err := testenv.NewTestPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	db, err := clone(t, "builtin_server")
	require.NoError(t, err)
	const org = "org_builtin_server_test"
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: org, Name: "Example", Slug: "builtin-server-example"})
	require.NoError(t, err)
	projects := projectrepo.New(db)
	first, err := projects.CreateProject(ctx, projectrepo.CreateProjectParams{OrganizationID: org, Name: "First", Slug: "first"})
	require.NoError(t, err)
	second, err := projects.CreateProject(ctx, projectrepo.CreateProjectParams{OrganizationID: org, Name: "Second", Slug: "second"})
	require.NoError(t, err)
	absent := uuid.NullUUID{}
	require.Error(t, EnsureServer(ctx, db, "other-org", first.ID, Slack, absent))
	require.Error(t, EnsureServer(ctx, db, org, first.ID, "unknown", absent))
	require.NoError(t, EnsureServer(ctx, db, org, first.ID, Slack, absent))
	require.NoError(t, EnsureServer(ctx, db, org, first.ID, Slack, absent))
	fixtures := testrepo.New(db)
	remoteID, ordinaryID := uuid.New(), uuid.New()
	require.NoError(t, fixtures.CreateRemoteMCPBackendFixture(ctx, testrepo.CreateRemoteMCPBackendFixtureParams{
		ID: remoteID, ProjectID: first.ID, Url: "https://upstream.example.invalid/mcp",
	}))
	require.NoError(t, fixtures.CreateRemoteBackedMCPServerFixture(ctx, testrepo.CreateRemoteBackedMCPServerFixtureParams{
		ID: ordinaryID, ProjectID: first.ID, RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true}, Visibility: "private",
	}))
	listed, err := mcprepo.New(db).ListMCPServersByProjectID(ctx, mcprepo.ListMCPServersByProjectIDParams{ProjectID: first.ID})
	require.NoError(t, err)
	require.Len(t, listed, 1, "built-in discovery must not leak through the ordinary server listing")
	require.Equal(t, ordinaryID, listed[0].ID)
	var name, visibility string
	var requires bool
	require.NoError(t, db.QueryRow(ctx, "SELECT name, visibility, requires_upstream_auth FROM mcp_servers WHERE id=$1", ID(first.ID, Slack)).Scan(&name, &visibility, &requires))
	require.Equal(t, "private", visibility)
	require.True(t, requires)
	_, err = db.Exec(ctx, "UPDATE mcp_servers SET name='Custom', visibility='public' WHERE id=$1", ID(first.ID, Slack))
	require.NoError(t, err)
	require.NoError(t, EnsureServer(ctx, db, org, first.ID, Slack, absent))
	require.NoError(t, db.QueryRow(ctx, "SELECT name, visibility FROM mcp_servers WHERE id=$1", ID(first.ID, Slack)).Scan(&name, &visibility))
	require.Equal(t, "Custom", name)
	require.Equal(t, "public", visibility)
	_, err = db.Exec(ctx, "UPDATE mcp_servers SET deleted_at=clock_timestamp() WHERE id=$1", ID(first.ID, Slack))
	require.NoError(t, err)
	require.Error(t, EnsureServer(ctx, db, org, first.ID, Slack, absent))
	var deleted bool
	require.NoError(t, db.QueryRow(ctx, "SELECT deleted FROM mcp_servers WHERE id=$1", ID(first.ID, Slack)).Scan(&deleted))
	require.True(t, deleted)
	_, err = db.Exec(ctx, "INSERT INTO mcp_servers(id, project_id, builtin_backend, visibility) VALUES($1,$2,'approvals','private')", ID(second.ID, Slack), first.ID)
	require.NoError(t, err)
	require.Error(t, EnsureServer(ctx, db, org, second.ID, Slack, absent))
	_, err = db.Exec(ctx, "UPDATE projects SET deleted_at=clock_timestamp() WHERE id=$1", first.ID)
	require.NoError(t, err)
	require.Error(t, EnsureServer(ctx, db, org, first.ID, Slack, absent))
}
