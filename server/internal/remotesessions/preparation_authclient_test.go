package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func assertPreparationTypedClient(t *testing.T, ctx context.Context, ti *testInstance, clientID uuid.UUID, resource string) uuid.UUID {
	t.Helper()
	auth, _ := contextvalues.GetAuthContext(ctx)
	ids, err := repo.New(ti.conn).ListPreparationTypedClientsFixture(ctx, repo.ListPreparationTypedClientsFixtureParams{OrganizationID: auth.ActiveOrganizationID, ClientID: conv.ToNullUUID(clientID), Resource: resource})
	require.NoError(t, err)
	require.Len(t, ids, 1, "normal preparation must publish exactly one typed EMA client without migration backfill")
	return ids[0]
}

func TestPrepareEMA_NormalTypedCatalog(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	serverID := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "normal-ema", in.Resource)
	payload := &clientsgen.PrepareEMAPayload{
		UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
		ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: in.Resource, Scopes: in.Scopes, Mechanism: "manual",
		ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
	}
	grants := func() int {
		var count int
		fixture1, fixtureErr := repo.New(ti.conn).CountPreparationPrincipalGrantsFixture(ctx, auth.ActiveOrganizationID)
		require.NoError(t, fixtureErr)
		count = int(fixture1)
		return count
	}
	before := grants()
	first, err := ti.service.PrepareEMA(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "ready", first.State)
	id := assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource)
	var linkID uuid.UUID
	fixture2, fixtureErr := repo.New(ti.conn).GetPreparationLiveLinkFixture(ctx, repo.GetPreparationLiveLinkFixtureParams{OrganizationID: auth.ActiveOrganizationID, ServerID: serverID, ClientID: id})
	require.NoError(t, fixtureErr)
	linkID = fixture2
	again, err := ti.service.PrepareEMA(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, first, again)
	require.Equal(t, id, assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource))
	require.Equal(t, before, grants(), "publishing a choice must not authorize any principal")

	// An administrator's disabled client and detached link are not undone by preparation.
	err = repo.New(ti.conn).DisablePreparationTypedClientFixture(ctx, repo.DisablePreparationTypedClientFixtureParams{OrganizationID: auth.ActiveOrganizationID, ClientID: id, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
	require.NoError(t, err)
	err = repo.New(ti.conn).DetachPreparationTypedLinkFixture(ctx, repo.DetachPreparationTypedLinkFixtureParams{OrganizationID: auth.ActiveOrganizationID, LinkID: linkID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	_, err = ti.service.PrepareEMA(ctx, payload)
	require.NoError(t, err)
	var enabled, detached bool
	fixture3, fixtureErr := repo.New(ti.conn).GetPreparationTypedLinkStateFixture(ctx, repo.GetPreparationTypedLinkStateFixtureParams{OrganizationID: auth.ActiveOrganizationID, ClientID: id, LinkID: linkID})
	require.NoError(t, fixtureErr)
	enabled, detached = fixture3.Enabled, fixture3.Deleted
	require.False(t, enabled)
	require.True(t, detached)
	var liveLinks int
	fixture4, fixtureErr := repo.New(ti.conn).CountPreparationLiveLinksFixture(ctx, repo.CountPreparationLiveLinksFixtureParams{OrganizationID: auth.ActiveOrganizationID, ServerID: serverID, ClientID: id})
	require.NoError(t, fixtureErr)
	liveLinks = int(fixture4)
	require.Zero(t, liveLinks, "preparation must not create a replacement for an administrator-detached link")
	require.Equal(t, id, assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource))
	require.Equal(t, before, grants())
}

func TestPrepareEMA_TypedCatalogFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "rollback-ema", in.Resource)
	// Each fixture owns an isolated database. Reject only typed EMA insertion.
	//nolint:glint // notestingrawsql: inject or remove a test-only constraint failure in an isolated database; SQLc cannot model DDL.
	_, err := ti.conn.Exec(ctx, `ALTER TABLE mcp_auth_clients ADD CONSTRAINT reject_test_ema CHECK (client_type <> 'ema')`)
	require.NoError(t, err)
	result, err := ti.service.PrepareEMA(ctx, &clientsgen.PrepareEMAPayload{
		UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
		ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: in.Resource, Scopes: in.Scopes, Mechanism: "manual",
		ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
	})
	require.Error(t, err, "sync errors must reach the caller, not become postcommit warnings")
	require.Nil(t, result)
	var bindings int
	fixture5, fixtureErr := repo.New(ti.conn).CountPreparationBindingsFixtureForUser(ctx, repo.CountPreparationBindingsFixtureForUserParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, UserIssuerID: in.UserSessionIssuerID})
	require.NoError(t, fixtureErr)
	bindings = int(fixture5)
	require.Zero(t, bindings, "the ready binding must roll back with typed-client failure")
	var grants []string
	fixture6, fixtureErr := repo.New(ti.conn).GetPreparationClientGrantsFixture(ctx, in.ClientID)
	require.NoError(t, fixtureErr)
	grants = fixture6
	require.NotContains(t, grants, oauthwire.GrantTypeJWTBearer, "grant confirmation must also roll back")
}
