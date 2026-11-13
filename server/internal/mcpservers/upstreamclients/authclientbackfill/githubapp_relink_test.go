package authclientbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
)

func TestSyncRemoteSource_GitHubReplacementAllowsHeaderRelink(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	source := f.source()
	server := f.serverOn(source)
	header := f.header(source, "Authorization", true, secretValue, "")
	f.sync(source)
	clients := f.live(TypeStaticCredential)
	require.Len(t, clients, 1)
	staticID := clients[0].ID
	require.Equal(t, []uuid.UUID{staticID}, f.links(server))
	headers := f.headers(staticID)
	require.Len(t, headers, 1)
	require.Equal(t, secretValue, headers[0].Value.String)

	queries := authclientsrepo.New(f.pool)
	app, err := queries.CreateGitHubAppAuthClient(ctx, authclientsrepo.CreateGitHubAppAuthClientParams{
		OrganizationID: f.orgID,
		ProjectID:      uuid.NullUUID{UUID: f.projectID, Valid: true},
		Name:           "Header replacement fixture",
	})
	require.NoError(t, err)
	require.NoError(t, queries.SetGitHubAppCredential(ctx, authclientsrepo.SetGitHubAppCredentialParams{
		AuthClientID: app.ID, OrganizationID: f.orgID, InstanceUrl: "https://github.com",
		AppID: 123, InstallationID: 456, PrivateKeyEncrypted: "test-encrypted-key", UpstreamOrigin: "https://mcp.example.com",
	}))
	require.NoError(t, queries.AttachGitHubAppAuthClient(ctx, authclientsrepo.AttachGitHubAppAuthClientParams{
		ProjectID: f.projectID, McpServerID: server, AuthClientID: app.ID, OrganizationID: f.orgID,
	}))
	require.NoError(t, queries.ReplaceServerAuthWithGitHubApp(ctx, authclientsrepo.ReplaceServerAuthWithGitHubAppParams{
		McpServerID: server, OrganizationID: f.orgID, AuthClientID: app.ID,
	}))
	require.Equal(t, []uuid.UUID{app.ID}, f.links(server))

	f.adminDetach(server, app.ID)
	require.Empty(t, f.links(server))
	require.NoError(t, f.q.UpdateRemoteHeaderFixture(ctx, UpdateRemoteHeaderFixtureParams{
		ID: header, Name: "Authorization", IsSecret: true,
		Value: pgtype.Text{String: rotatedValue, Valid: true}, ValueFromRequestHeader: pgtype.Text{},
	}))
	f.sync(source)

	require.Equal(t, []uuid.UUID{staticID}, f.links(server), "replacement must not turn the backfill link into an explicit admin detach")
	clients = f.live(TypeStaticCredential)
	require.Len(t, clients, 1)
	require.Equal(t, staticID, clients[0].ID)
	headers = f.headers(staticID)
	require.Len(t, headers, 1, "the in-place edit replaces the old credential rather than retaining it")
	require.Equal(t, "Authorization", headers[0].Name)
	require.Equal(t, pgtype.Text{String: rotatedValue, Valid: true}, headers[0].Value)
}
