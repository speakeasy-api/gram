package slackdirectoryconnections_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
)

func TestOrgEncryptedWorkspaceCredentialsAreReadFromTheOrganizationCopy(t *testing.T) {
	t.Parallel()
	ctx, f := newOrgEncryptedService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)

	row := connection(t, ctx, f, input)
	require.True(t, row.CredentialsEncrypted.Valid, "the application-key copy is kept for rollback")
	require.True(t, row.CredentialsOrgEncrypted.Valid)
	require.NotContains(t, row.CredentialsOrgEncrypted.String, "synthetic-access-token")

	// With the application-key copy unreadable, the sync can only get the
	// token through the organization copy.
	_, err := repo.New(f.db).UpdateSlackDirectoryCredentials(ctx, repo.UpdateSlackDirectoryCredentialsParams{
		CredentialsEncrypted: conv.ToPGText("not-a-ciphertext"), CredentialsOrgEncrypted: row.CredentialsOrgEncrypted,
		OrganizationID: input.OrganizationID, ID: input.ConnectionID, Generation: input.Generation,
	})
	require.NoError(t, err)
	require.NoError(t, syncer(f, tokenCheckingSnapshot(t, "synthetic-access-token")).Run(ctx, input, nil))
	require.Len(t, members(t, ctx, f), 1)

	disconnected, err := f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: c.ID, Generation: c.Generation})
	require.NoError(t, err)
	row, err = repo.New(f.db).GetSlackDirectoryConnection(ctx, repo.GetSlackDirectoryConnectionParams{OrganizationID: f.auth.ActiveOrganizationID, ID: uuid.MustParse(disconnected.ID)})
	require.NoError(t, err)
	require.False(t, row.CredentialsEncrypted.Valid)
	require.False(t, row.CredentialsOrgEncrypted.Valid, "disconnect clears both copies")
}

func TestOrgEncryptedWorkspaceStaysConnectedWhileTheOrganizationKeyIsUnavailable(t *testing.T) {
	t.Parallel()
	ctx, f := newOrgEncryptedService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	require.NoError(t, f.orgKeys.Provisioner.Suspend(ctx, f.auth.ActiveOrganizationID, "customer disabled the key", orgencryption.SystemActor()))

	err := syncer(f, snapshot("UEXAMPLE01")).Run(ctx, syncRequest(f, c), nil)
	var syncErr *slackdirectoryconnections.SyncError
	require.ErrorAs(t, err, &syncErr)
	require.Equal(t, "encryption_unavailable", syncErr.Code)
	require.True(t, syncErr.Retryable)
	require.False(t, syncErr.Reconnect, "reconnecting cannot fix an unavailable key")

	list, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, list.Connections, 1)
	require.Equal(t, "connected", list.Connections[0].Status)
}
