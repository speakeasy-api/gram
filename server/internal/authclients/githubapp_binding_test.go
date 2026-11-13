package authclients_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/stretchr/testify/require"
)

func TestGetGitHubAppCredentialLiveBinding(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"live", "detached", "replaced", "disabled", "deleted", "wrong server", "wrong tenant", "foreign server"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			queries := repo.New(ti.conn)
			server := seedServer(t, ctx, ti.conn, ti.projectID, "private")
			createClient := func() uuid.UUID {
				t.Helper()
				client, err := queries.CreateGitHubAppAuthClient(ctx, repo.CreateGitHubAppAuthClientParams{
					OrganizationID: ti.organizationID,
					ProjectID:      uuid.NullUUID{UUID: ti.projectID, Valid: true},
					Name:           "Binding fixture",
				})
				require.NoError(t, err)
				require.NoError(t, queries.SetGitHubAppCredential(ctx, repo.SetGitHubAppCredentialParams{
					AuthClientID:        client.ID,
					OrganizationID:      ti.organizationID,
					InstanceUrl:         "https://git.example.com",
					AppID:               123,
					InstallationID:      456,
					PrivateKeyEncrypted: "TEST_ENCRYPTED_PRIVATE_KEY",
					UpstreamOrigin:      "https://upstream.example.com",
				}))
				associate(t, ctx, ti.conn, ti.organizationID, server, client.ID)
				return client.ID
			}
			clientID := createClient()
			params := repo.GetGitHubAppCredentialParams{AuthClientID: clientID, OrganizationID: ti.organizationID, McpServerID: server.id}
			stored, err := queries.GetGitHubAppCredential(ctx, params)
			require.NoError(t, err)
			require.Equal(t, clientID, stored.AuthClientID)
			require.Equal(t, "TEST_ENCRYPTED_PRIVATE_KEY", stored.PrivateKeyEncrypted)

			switch state {
			case "live":
				return
			case "detached":
				_, err := queries.DetachAuthClientFromServer(ctx, repo.DetachAuthClientFromServerParams{
					AuthClientID: clientID, McpServerID: server.id, OrganizationID: ti.organizationID,
				})
				require.NoError(t, err)
			case "replaced":
				replacementID := createClient()
				require.NoError(t, queries.ReplaceServerAuthWithGitHubApp(ctx, repo.ReplaceServerAuthWithGitHubAppParams{
					McpServerID: server.id, OrganizationID: ti.organizationID, AuthClientID: replacementID,
				}))
				replacement, err := queries.GetGitHubAppCredential(ctx, repo.GetGitHubAppCredentialParams{
					AuthClientID: replacementID, OrganizationID: ti.organizationID, McpServerID: server.id,
				})
				require.NoError(t, err)
				require.Equal(t, replacementID, replacement.AuthClientID)
			case "deleted":
				require.NoError(t, authclientfixtures.New(ti.conn).SoftDeleteAuthClientFixture(ctx, clientID))
			case "disabled":
				_, err := queries.UpdateAuthClient(ctx, repo.UpdateAuthClientParams{
					ID: clientID, OrganizationID: ti.organizationID, Name: "Disabled binding fixture", Enabled: false,
				})
				require.NoError(t, err)
			case "wrong server":
				otherServer := seedServer(t, ctx, ti.conn, ti.projectID, "private")
				params.McpServerID = otherServer.id
			case "wrong tenant":
				otherOrganizationID, _ := seedOtherOrganization(t, ctx, ti.conn)
				params.OrganizationID = otherOrganizationID
			case "foreign server":
				otherOrganizationID, otherProjectID := seedOtherOrganization(t, ctx, ti.conn)
				otherServer := seedServer(t, ctx, ti.conn, otherProjectID, "private")
				params.OrganizationID = otherOrganizationID
				params.McpServerID = otherServer.id
			}
			_, err = queries.GetGitHubAppCredential(ctx, params)
			require.ErrorIs(t, err, pgx.ErrNoRows)
		})
	}
}

func TestGitHubAppRequirementIgnoresDeletedTypedReplacement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	q := repo.New(ti.conn)
	server := seedServer(t, ctx, ti.conn, ti.projectID, "private")
	owner := uuid.NullUUID{UUID: ti.projectID, Valid: true}
	app, err := q.CreateGitHubAppAuthClient(ctx, repo.CreateGitHubAppAuthClientParams{OrganizationID: ti.organizationID, ProjectID: owner, Name: "App fixture"})
	require.NoError(t, err)
	require.NoError(t, q.SetGitHubAppCredential(ctx, repo.SetGitHubAppCredentialParams{AuthClientID: app.ID, OrganizationID: ti.organizationID, InstanceUrl: "https://github.com", AppID: 123, InstallationID: 456, PrivateKeyEncrypted: "TEST_KEY", UpstreamOrigin: "https://upstream.example.com"}))
	associate(t, ctx, ti.conn, ti.organizationID, server, app.ID)
	_, err = q.DetachAuthClientFromServer(ctx, repo.DetachAuthClientFromServerParams{AuthClientID: app.ID, McpServerID: server.id, OrganizationID: ti.organizationID})
	require.NoError(t, err)
	replacement, err := q.CreateAuthClient(ctx, repo.CreateAuthClientParams{OrganizationID: ti.organizationID, ProjectID: owner, ClientType: "static_credential", Name: "Replacement fixture"})
	require.NoError(t, err)
	associate(t, ctx, ti.conn, ti.organizationID, server, replacement.ID)
	params := repo.ListGitHubAppServersParams{OrganizationID: ti.organizationID, McpServerIds: []uuid.UUID{server.id}}
	required, err := q.ListGitHubAppServers(ctx, params)
	require.NoError(t, err)
	require.Empty(t, required, "a live explicitly attached client replaces the App")
	require.NoError(t, authclientfixtures.New(ti.conn).SoftDeleteAuthClientFixture(ctx, replacement.ID))
	required, err = q.ListGitHubAppServers(ctx, params)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{server.id}, required, "a dangling live association to a deleted client cannot erase the requirement")
}
