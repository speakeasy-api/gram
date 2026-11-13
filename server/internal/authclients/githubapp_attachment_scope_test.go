package authclients_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestAttachGitHubAppAuthClientScopesTarget(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"live", "missing server", "foreign server", "mismatched project", "deleted server", "client project mismatch", "organization client across projects"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			q := repo.New(ti.conn)
			server := seedServer(t, ctx, ti.conn, ti.projectID, "private")
			owner := uuid.NullUUID{UUID: ti.projectID, Valid: true}
			if state == "organization client across projects" {
				owner = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
			}
			client, err := q.CreateGitHubAppAuthClient(ctx, repo.CreateGitHubAppAuthClientParams{OrganizationID: ti.organizationID, ProjectID: owner, Name: "Scoped attachment fixture"})
			require.NoError(t, err)
			params := repo.AttachGitHubAppAuthClientParams{McpServerID: server.id, AuthClientID: client.ID, OrganizationID: ti.organizationID, ProjectID: ti.projectID}
			switch state {
			case "missing server":
				params.McpServerID = uuid.New()
			case "foreign server":
				_, projectID := seedOtherOrganization(t, ctx, ti.conn)
				server = seedServer(t, ctx, ti.conn, projectID, "private")
				params.McpServerID, params.ProjectID = server.id, projectID
			case "mismatched project":
				_, params.ProjectID = seedOtherOrganization(t, ctx, ti.conn)
			case "deleted server":
				require.NoError(t, authclientfixtures.New(ti.conn).SoftDeleteMCPServerFixture(ctx, server.id))
			case "client project mismatch", "organization client across projects":
				id := uuid.New()
				projectID, err := testrepo.New(ti.conn).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: id, Name: "Other project", Slug: "p-" + id.String(), OrganizationID: ti.organizationID})
				require.NoError(t, err)
				server = seedServer(t, ctx, ti.conn, projectID, "private")
				params.McpServerID, params.ProjectID = server.id, projectID
			}
			require.NoError(t, q.AttachGitHubAppAuthClient(ctx, params), "invalid targets are not inserted")
			clients, err := q.ListServerAuthClients(ctx, repo.ListServerAuthClientsParams{McpServerID: params.McpServerID, ProjectID: server.projectID, OrganizationID: ti.organizationID})
			require.NoError(t, err)
			if state == "live" || state == "organization client across projects" {
				require.Len(t, clients, 1)
				require.Equal(t, client.ID, clients[0].ID)
			} else {
				require.Empty(t, clients)
			}
		})
	}
}
