package authclients_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
)

// An administrator's detach outlives the backfill: rerunning it must not
// relink the server from the upstream OAuth association it was derived from,
// and must leave the client's other servers and the association itself alone.
func TestDetachedBackfilledOAuthClientStaysDetachedAcrossBackfillRuns(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	second := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)

	fixtures := authclientbackfill.New(ti.conn)
	issuerID, upstreamClientID := uuid.New(), uuid.New()
	require.NoError(t, fixtures.SeedRemoteSessionIssuerFixture(ctx, authclientbackfill.SeedRemoteSessionIssuerFixtureParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		Slug:      "rsi-" + issuerID.String()[:8],
		Issuer:    "https://auth.example.com/" + issuerID.String()[:8],
		Name:      pgtype.Text{String: "Example", Valid: true},
	}))
	require.NoError(t, fixtures.SeedRemoteSessionClientFixture(ctx, authclientbackfill.SeedRemoteSessionClientFixtureParams{
		ID:                      upstreamClientID,
		ProjectID:               uuid.NullUUID{UUID: ti.projectID, Valid: true},
		OrganizationID:          pgtype.Text{String: ti.organizationID, Valid: true},
		RemoteSessionIssuerID:   issuerID,
		ClientID:                "cid-" + upstreamClientID.String()[:8],
		CredentialOwner:         "subject",
		TokenEndpointAuthMethod: pgtype.Text{String: "", Valid: false},
	}))
	for _, server := range []serverFixture{first, second} {
		_, err := fixtures.SeedAssociationFixture(ctx, authclientbackfill.SeedAssociationFixtureParams{
			ProjectID:             ti.projectID,
			OrganizationID:        ti.organizationID,
			McpServerID:           server.id,
			RemoteSessionClientID: upstreamClientID,
		})
		require.NoError(t, err)
	}

	runBackfill := func(apply bool) authclientbackfill.Report {
		t.Helper()
		report, err := authclientbackfill.Run(ctx, ti.conn, authclientbackfill.Options{OrganizationID: ti.organizationID, Apply: apply, PageSize: 0})
		require.NoError(t, err)
		return report
	}
	runBackfill(true)

	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	serverIDs := func(client *types.AuthClient) []string {
		t.Helper()
		ids := make([]string, 0, len(client.Servers))
		for _, server := range client.Servers {
			ids = append(ids, server.ID)
		}
		return ids
	}

	listed, err := ti.service.ListAuthClients(ctx, &gen.ListAuthClientsPayload{McpServerID: new(first.id.String()), ClientType: nil, Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	clientID := listed.Items[0].ID
	require.ElementsMatch(t, []string{first.id.String(), second.id.String()}, serverIDs(listed.Items[0]))

	_, err = ti.service.DetachAuthClientFromMcpServer(ctx, &gen.DetachAuthClientFromMcpServerPayload{ID: clientID, McpServerID: first.id.String(), SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)

	dryRun := runBackfill(false)
	require.Zero(t, dryRun.PendingLinks, "a dry run must not plan to relink a detached server")
	require.Zero(t, dryRun.Gaps)

	rerun := runBackfill(true)
	require.Zero(t, rerun.LinksWritten, "the backfill must not relink a detached server")
	require.Zero(t, rerun.Gaps)

	after, err := ti.service.GetAuthClient(ctx, &gen.GetAuthClientPayload{ID: clientID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, []string{second.id.String()}, serverIDs(after), "the detach holds and the other server keeps the client")

	// The detach changes only the typed link: both upstream OAuth
	// associations it was derived from stay live.
	associations, err := fixtures.ListAuthClientBackfillCandidates(ctx, authclientbackfill.ListAuthClientBackfillCandidatesParams{
		Cursor:         uuid.Nil,
		OrganizationID: ti.organizationID,
		PageSize:       100,
	})
	require.NoError(t, err)
	associatedServers := make([]uuid.UUID, 0, len(associations))
	for _, association := range associations {
		if association.RemoteSessionClientID == upstreamClientID {
			associatedServers = append(associatedServers, association.McpServerID)
		}
	}
	require.ElementsMatch(t, []uuid.UUID{first.id, second.id}, associatedServers)
}
