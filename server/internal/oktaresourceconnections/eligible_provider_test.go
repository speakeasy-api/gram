package oktaresourceconnections_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// eligibleIssuers returns the provider each query reports for serverID, uuid.Nil
// when it leaves the server out, and fails on a duplicate list row.
func eligibleIssuers(t *testing.T, ctx context.Context, si *instance, serverID uuid.UUID) (listed, got uuid.UUID) {
	t.Helper()
	rows, err := si.q.ListEligibleServers(ctx, repo.ListEligibleServersParams{TypedAuthEnforced: false, OrganizationID: si.orgID})
	require.NoError(t, err)
	for _, row := range rows {
		if row.ID == serverID {
			require.Equal(t, uuid.Nil, listed, "one list row per server")
			listed = row.IssuerID
		}
	}
	row, err := si.q.GetEligibleServer(ctx, repo.GetEligibleServerParams{OrganizationID: si.orgID, McpServerID: serverID})
	if err != nil {
		require.ErrorIs(t, err, pgx.ErrNoRows)
		return listed, uuid.Nil
	}
	return listed, row.IssuerID
}

func TestEligibleServers_OneProvider(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	f := capableServer(t, ctx, si, "One")

	listed, got := eligibleIssuers(t, ctx, si, f.serverID)
	require.Equal(t, f.issuerID, listed)
	require.Equal(t, f.issuerID, got)
}

func TestEligibleServers_NoAssociationLeavesServerOut(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	f := capableServer(t, ctx, si, "None")
	require.NoError(t, testrepo.New(si.conn).ClearMCPServerRemoteSessionClientsFixture(ctx, f.serverID))

	listed, got := eligibleIssuers(t, ctx, si, f.serverID)
	require.Equal(t, uuid.Nil, listed, "the column alone does not make a server eligible")
	require.Equal(t, uuid.Nil, got)
}

func TestEligibleServers_AssociationOverridesColumn(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	f := capableServer(t, ctx, si, "Stale")
	other := createResourceIssuer(t, ctx, si, si.orgID, f.projectID, true)
	_, err := si.q.SetMCPServerIssuerFixture(ctx, repo.SetMCPServerIssuerFixtureParams{RemoteSessionIssuerID: uuid.NullUUID{UUID: other, Valid: true}, ID: f.serverID, ProjectID: f.projectID})
	require.NoError(t, err)

	listed, got := eligibleIssuers(t, ctx, si, f.serverID)
	require.Equal(t, f.issuerID, listed)
	require.Equal(t, f.issuerID, got)
}

func TestEligibleServers_SeveralProvidersLeaveServerOut(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	f := capableServer(t, ctx, si, "Several")
	other := createResourceIssuer(t, ctx, si, si.orgID, f.projectID, true)
	associateServerIssuer(t, ctx, si, f.projectID, f.serverID, other)

	listed, got := eligibleIssuers(t, ctx, si, f.serverID)
	require.Equal(t, uuid.Nil, listed)
	require.Equal(t, uuid.Nil, got)
}

func TestEligibleServers_SeveralClientsOfOneProviderKeepOneRow(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	f := capableServer(t, ctx, si, "Clients")
	second := addClient(t, ctx, si, f.issuerID, uuid.NullUUID{UUID: f.projectID, Valid: true}, "0oasecond")
	require.NoError(t, testrepo.New(si.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{
		ProjectID:             f.projectID,
		OrganizationID:        si.orgID,
		McpServerID:           f.serverID,
		RemoteSessionClientID: second,
	}))

	listed, got := eligibleIssuers(t, ctx, si, f.serverID)
	require.Equal(t, f.issuerID, listed)
	require.Equal(t, f.issuerID, got)
}

func TestEligibleServers_DetachedAssociationLeavesServerOut(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	projectID := createProject(t, ctx, si, si.orgID, "proj-"+uuid.NewString()[:8])
	issuerID := createResourceIssuer(t, ctx, si, si.orgID, projectID, true)
	clientID := addClient(t, ctx, si, issuerID, uuid.NullUUID{UUID: projectID, Valid: true}, "0oadetached")
	serverID := createServer(t, ctx, si, projectID, issuerID, "Detached")

	detached, err := upstreamclientsrepo.New(si.conn).DetachMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.DetachMCPServerRemoteSessionClientParams{
		McpServerID:           serverID,
		RemoteSessionClientID: clientID,
		ProjectID:             projectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), detached)

	listed, got := eligibleIssuers(t, ctx, si, serverID)
	require.Equal(t, uuid.Nil, listed)
	require.Equal(t, uuid.Nil, got)
}

func TestEligibleServers_DeletedClientLeavesServerOut(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	projectID := createProject(t, ctx, si, si.orgID, "proj-"+uuid.NewString()[:8])
	issuerID := createResourceIssuer(t, ctx, si, si.orgID, projectID, true)
	clientID := addClient(t, ctx, si, issuerID, uuid.NullUUID{UUID: projectID, Valid: true}, "0oadeleted")
	serverID := createServer(t, ctx, si, projectID, issuerID, "DeletedClient")

	deleted, err := testrepo.New(si.conn).SoftDeleteRemoteSessionClientFixture(ctx, testrepo.SoftDeleteRemoteSessionClientFixtureParams{
		ID:             clientID,
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID: conv.ToPGText(si.orgID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	listed, got := eligibleIssuers(t, ctx, si, serverID)
	require.Equal(t, uuid.Nil, listed)
	require.Equal(t, uuid.Nil, got)
}

func TestEligibleServers_DeletedProviderLeavesServerOut(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	f := capableServer(t, ctx, si, "DeletedProvider")
	require.NoError(t, testrepo.New(si.conn).ForceSoftDeleteRemoteSessionIssuerFixture(ctx, f.issuerID))

	listed, got := eligibleIssuers(t, ctx, si, f.serverID)
	require.Equal(t, uuid.Nil, listed)
	require.Equal(t, uuid.Nil, got)
}

func TestEligibleServers_DeletedProviderDoesNotCountTowardSeveral(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	f := capableServer(t, ctx, si, "LiveAndDeleted")
	deleted := createResourceIssuer(t, ctx, si, si.orgID, f.projectID, true)
	associateServerIssuer(t, ctx, si, f.projectID, f.serverID, deleted)
	require.NoError(t, testrepo.New(si.conn).ForceSoftDeleteRemoteSessionIssuerFixture(ctx, deleted))

	listed, got := eligibleIssuers(t, ctx, si, f.serverID)
	require.Equal(t, f.issuerID, listed)
	require.Equal(t, f.issuerID, got)
}
