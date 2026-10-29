package tunneledmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	backfill "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type tunneledEMAFixture struct {
	ctx                               context.Context
	ti                                *testInstance
	source, server, typed, standalone uuid.UUID
	resource                          string
}

func prepareTunneledEMA(t *testing.T) tunneledEMAFixture {
	t.Helper()
	ctx, ti := newTestService(t)
	auth := requireAuthContext(t, ctx)
	ctx = authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *auth.ProjectID))
	resource := "https://tunneled.example.test/mcp"
	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{Name: "EMA tunneled source", ResourceIdentifier: new(resource)})
	require.NoError(t, err)
	source := uuid.MustParse(created.Server.ID)
	q := backfill.New(ti.conn)
	usi, issuer, client, server := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, q.SeedUserSessionIssuerFixture(ctx, backfill.SeedUserSessionIssuerFixtureParams{ID: usi, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, Slug: "usi-" + usi.String()}))
	require.NoError(t, q.SeedRemoteSessionIssuerFixture(ctx, backfill.SeedRemoteSessionIssuerFixtureParams{ID: issuer, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, Slug: "rsi-" + issuer.String(), Issuer: "https://issuer.example.test/" + issuer.String()}))
	require.NoError(t, q.SeedRemoteSessionClientFixture(ctx, backfill.SeedRemoteSessionClientFixtureParams{ID: client, ProjectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true}, OrganizationID: pgtype.Text{String: auth.ActiveOrganizationID, Valid: true}, RemoteSessionIssuerID: issuer, ClientID: "client-" + client.String(), CredentialOwner: "self", TokenEndpointAuthMethod: pgtype.Text{String: "client_secret_basic", Valid: true}}))
	require.NoError(t, q.SeedTunneledMCPServerFixture(ctx, backfill.SeedTunneledMCPServerFixtureParams{ID: server, ProjectID: *auth.ProjectID, Slug: pgtype.Text{String: "server-" + server.String(), Valid: true}, TunneledMcpServerID: uuid.NullUUID{UUID: source, Valid: true}}))
	require.NoError(t, q.SetServerUserSessionIssuerFixture(ctx, backfill.SetServerUserSessionIssuerFixtureParams{ID: server, UserSessionIssuerID: uuid.NullUUID{UUID: usi, Valid: true}}))
	require.NoError(t, q.SetServerRemoteSessionIssuerFixture(ctx, backfill.SetServerRemoteSessionIssuerFixtureParams{ID: server, RemoteSessionIssuerID: uuid.NullUUID{UUID: issuer, Valid: true}}))
	_, err = q.SeedTunneledHeaderFixture(ctx, backfill.SeedTunneledHeaderFixtureParams{TunneledMcpServerID: source, Name: "X-Enterprise-Key", IsSecret: true, Value: pgtype.Text{String: "test-secret", Valid: true}})
	require.NoError(t, err)
	binding, err := q.SeedEMABindingFixture(ctx, backfill.SeedEMABindingFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: usi, RemoteSessionIssuerID: issuer, RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true}, Resource: resource, RequestedScopes: []string{"read"}})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, backfill.SyncEMABinding(ctx, tx, auth.ActiveOrganizationID, binding))
	require.NoError(t, tx.Commit(ctx))
	return tunneledEMAFixture{ctx: ctx, ti: ti, source: source, server: server, typed: backfill.EMAClientID(auth.ActiveOrganizationID, issuer, client, resource), standalone: backfill.SourceClientID(auth.ActiveOrganizationID, backfill.SourceKindTunneled, source, backfill.TypeStaticCredential), resource: resource}
}

func (f tunneledEMAFixture) assertEMA(t *testing.T, live bool) {
	t.Helper()
	q := backfill.New(f.ti.conn)
	client, err := q.GetAuthClientFixture(f.ctx, f.typed)
	require.NoError(t, err)
	require.Equal(t, !live, client.Deleted)
	links, err := q.ListBackfillLinkedServersFixture(f.ctx, f.typed)
	require.NoError(t, err)
	headers, err := q.ListAuthClientHeadersFixture(f.ctx, f.typed)
	require.NoError(t, err)
	if live {
		require.Equal(t, []uuid.UUID{f.server}, links)
		require.Len(t, headers, 1)
	} else {
		require.Empty(t, links)
		require.Empty(t, headers)
	}
}

func TestUpdateServer_ReconcilesTunneledEMAResourceReach(t *testing.T) {
	t.Parallel()
	f := prepareTunneledEMA(t)
	f.assertEMA(t, true)
	_, err := f.ti.service.UpdateServer(f.ctx, &gen.UpdateServerPayload{ID: f.source.String(), ResourceIdentifier: new(f.resource + "/different")})
	require.NoError(t, err)
	f.assertEMA(t, false)
	links, err := backfill.New(f.ti.conn).ListBackfillLinkedServersFixture(f.ctx, f.standalone)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{f.server}, links)
	_, err = f.ti.service.UpdateServer(f.ctx, &gen.UpdateServerPayload{ID: f.source.String(), ResourceIdentifier: new(f.resource)})
	require.NoError(t, err)
	f.assertEMA(t, true)
	links, err = backfill.New(f.ti.conn).ListBackfillLinkedServersFixture(f.ctx, f.standalone)
	require.NoError(t, err)
	require.Empty(t, links)
}

func TestDeleteServer_RetiresTunneledEMAOwnership(t *testing.T) {
	t.Parallel()
	f := prepareTunneledEMA(t)
	f.assertEMA(t, true)
	require.NoError(t, f.ti.service.DeleteServer(f.ctx, &gen.DeleteServerPayload{ID: f.source.String()}))
	f.assertEMA(t, false)
	links, err := backfill.New(f.ti.conn).ListBackfillLinkedServersFixture(f.ctx, f.standalone)
	require.NoError(t, err)
	require.Empty(t, links)
}
