package builtinmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp/repo"
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
	builtins := repo.New(db)
	server, err := builtins.GetBuiltinServerFixture(ctx, repo.GetBuiltinServerFixtureParams{ID: ID(first.ID, Slack), ProjectID: first.ID})
	require.NoError(t, err)
	require.Equal(t, "private", server.Visibility)
	require.True(t, server.RequiresUpstreamAuth.Valid)
	require.True(t, server.RequiresUpstreamAuth.Bool)
	_, err = mcprepo.New(db).UpdateMCPServer(ctx, mcprepo.UpdateMCPServerParams{
		ID: server.ID, ProjectID: first.ID, Name: pgtype.Text{String: "Custom", Valid: true}, Visibility: "public",
	})
	require.NoError(t, err)
	require.NoError(t, EnsureServer(ctx, db, org, first.ID, Slack, absent))
	server, err = builtins.GetBuiltinServerFixture(ctx, repo.GetBuiltinServerFixtureParams{ID: ID(first.ID, Slack), ProjectID: first.ID})
	require.NoError(t, err)
	require.Equal(t, "Custom", server.Name.String)
	require.Equal(t, "public", server.Visibility)
	_, err = mcprepo.New(db).DeleteMCPServer(ctx, mcprepo.DeleteMCPServerParams{ID: server.ID, ProjectID: first.ID})
	require.NoError(t, err)
	require.Error(t, EnsureServer(ctx, db, org, first.ID, Slack, absent))
	server, err = builtins.GetBuiltinServerFixture(ctx, repo.GetBuiltinServerFixtureParams{ID: ID(first.ID, Slack), ProjectID: first.ID})
	require.NoError(t, err)
	require.True(t, server.Deleted)
	err = builtins.CreateBuiltinServerFixture(ctx, repo.CreateBuiltinServerFixtureParams{ID: ID(second.ID, Slack), ProjectID: first.ID, BuiltinBackend: "approvals"})
	require.NoError(t, err)
	require.Error(t, EnsureServer(ctx, db, org, second.ID, Slack, absent))
	_, err = projects.DeleteProject(ctx, first.ID)
	require.NoError(t, err)
	require.Error(t, EnsureServer(ctx, db, org, first.ID, Slack, absent))
}
