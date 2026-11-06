package metamcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// memberProviderFixture drives add, remove and issuer changes for one gateway.
type memberProviderFixture struct {
	ti              *testInstance
	projectID       uuid.UUID
	organizationID  string
	metaID          string
	metaName        string
	gatewayIssuerID uuid.UUID
}

func newMemberProviderFixture(t *testing.T) (context.Context, memberProviderFixture) {
	t.Helper()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	meta := seedMetaMcpServer(t, ctx, ti, "member provider gateway")
	require.NotNil(t, meta.UserSessionIssuerID)
	return ctx, memberProviderFixture{
		ti:              ti,
		projectID:       *authCtx.ProjectID,
		organizationID:  authCtx.ActiveOrganizationID,
		metaID:          meta.ID,
		metaName:        meta.Name,
		gatewayIssuerID: uuid.MustParse(*meta.UserSessionIssuerID),
	}
}

// addProvider gives server a client of a new provider, through its issuer and
// an association, and returns the provider.
func (f memberProviderFixture) addProvider(t *testing.T, ctx context.Context, serverID uuid.UUID) uuid.UUID {
	t.Helper()
	provider := seedRemoteSessionIssuer(t, ctx, f.ti.conn, f.projectID, f.organizationID, "member-provider")
	client := createRemoteSessionClient(t, ctx, f.ti.conn, f.projectID, f.organizationID, provider, "client-"+uuid.NewString()[:8])
	stampAndWireMemberClient(t, ctx, f.ti.conn, f.projectID, f.organizationID, serverID, client)
	return provider
}

// setColumn overwrites the server's denormalized provider, to prove readers no
// longer follow it.
func (f memberProviderFixture) setColumn(t *testing.T, ctx context.Context, serverID uuid.UUID, provider uuid.NullUUID) {
	t.Helper()
	stamped, err := testrepo.New(f.ti.conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		RemoteSessionIssuerID: provider,
		ID:                    serverID,
		ProjectID:             f.projectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stamped)
}

func (f memberProviderFixture) add(t *testing.T, ctx context.Context, serverID uuid.UUID) string {
	t.Helper()
	member, err := f.ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		MetaMcpServerID: f.metaID,
		McpServerID:     serverID.String(),
	})
	require.NoError(t, err)
	return member.ID
}

func (f memberProviderFixture) remove(t *testing.T, ctx context.Context, memberID string) {
	t.Helper()
	require.NoError(t, f.ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{ID: memberID}))
}

func (f memberProviderFixture) bound(t *testing.T, ctx context.Context, issuerID, provider uuid.UUID) int {
	t.Helper()
	return countGatewayClientsForUpstream(t, ctx, f.ti.conn, f.projectID, f.organizationID, issuerID, provider)
}

func TestMemberProvider_AddAndRemoveFollowTheAssociationNotTheColumn(t *testing.T) {
	t.Parallel()
	ctx, f := newMemberProviderFixture(t)

	server := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	stale := seedRemoteSessionIssuer(t, ctx, f.ti.conn, f.projectID, f.organizationID, "stale-provider")
	provider := f.addProvider(t, ctx, server)
	f.setColumn(t, ctx, server, conv.ToNullUUID(stale))

	member := f.add(t, ctx, server)
	require.Equal(t, 1, f.bound(t, ctx, f.gatewayIssuerID, provider), "add binds the associated provider")
	require.Equal(t, 0, f.bound(t, ctx, f.gatewayIssuerID, stale), "add ignores the column")

	f.remove(t, ctx, member)
	require.Equal(t, 0, f.bound(t, ctx, f.gatewayIssuerID, provider), "remove unbinds the associated provider")
}

func TestMemberProvider_ColumnNullStillWiresConsent(t *testing.T) {
	t.Parallel()
	ctx, f := newMemberProviderFixture(t)

	server := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	provider := f.addProvider(t, ctx, server)
	f.setColumn(t, ctx, server, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	f.add(t, ctx, server)
	require.Equal(t, 1, f.bound(t, ctx, f.gatewayIssuerID, provider))
}

func TestMemberProvider_SeveralProvidersWireNothing(t *testing.T) {
	t.Parallel()
	ctx, f := newMemberProviderFixture(t)

	server := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	first := f.addProvider(t, ctx, server)
	second := f.addProvider(t, ctx, server)
	f.setColumn(t, ctx, server, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	f.add(t, ctx, server)
	require.Equal(t, 0, f.bound(t, ctx, f.gatewayIssuerID, first))
	require.Equal(t, 0, f.bound(t, ctx, f.gatewayIssuerID, second))

	newIssuer := seedUserSessionIssuer(t, ctx, f.ti.conn, f.projectID)
	_, err := f.ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		ID:                  f.metaID,
		Name:                f.metaName,
		UserSessionIssuerID: conv.PtrEmpty(newIssuer.String()),
	})
	require.NoError(t, err)
	require.Equal(t, 0, f.bound(t, ctx, newIssuer, first), "an issuer change skips a member with several providers")
	require.Equal(t, 0, f.bound(t, ctx, newIssuer, second), "an issuer change skips a member with several providers")
}

func TestMemberProvider_IssuerChangeBindsTheAssociatedProvider(t *testing.T) {
	t.Parallel()
	ctx, f := newMemberProviderFixture(t)

	server := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	stale := seedRemoteSessionIssuer(t, ctx, f.ti.conn, f.projectID, f.organizationID, "stale-provider")
	provider := f.addProvider(t, ctx, server)
	f.add(t, ctx, server)
	f.setColumn(t, ctx, server, conv.ToNullUUID(stale))

	newIssuer := seedUserSessionIssuer(t, ctx, f.ti.conn, f.projectID)
	_, err := f.ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		ID:                  f.metaID,
		Name:                f.metaName,
		UserSessionIssuerID: conv.PtrEmpty(newIssuer.String()),
	})
	require.NoError(t, err)
	require.Equal(t, 1, f.bound(t, ctx, newIssuer, provider))
	require.Equal(t, 0, f.bound(t, ctx, newIssuer, stale))
}

func TestMemberProvider_RemoveKeepsBindingForAnAssociatedConsumer(t *testing.T) {
	t.Parallel()
	ctx, f := newMemberProviderFixture(t)

	leaving := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	provider := f.addProvider(t, ctx, leaving)
	leavingMember := f.add(t, ctx, leaving)

	staying := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	client := createRemoteSessionClient(t, ctx, f.ti.conn, f.projectID, f.organizationID, provider, "staying-client")
	stampAndWireMemberClient(t, ctx, f.ti.conn, f.projectID, f.organizationID, staying, client)
	f.add(t, ctx, staying)
	f.setColumn(t, ctx, staying, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	f.remove(t, ctx, leavingMember)
	require.Equal(t, 1, f.bound(t, ctx, f.gatewayIssuerID, provider), "the remaining member is associated with the provider")
}

func TestMemberProvider_RemoveIgnoresAConsumerNamedOnlyByTheColumn(t *testing.T) {
	t.Parallel()
	ctx, f := newMemberProviderFixture(t)

	leaving := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	provider := f.addProvider(t, ctx, leaving)
	leavingMember := f.add(t, ctx, leaving)

	staying := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	f.add(t, ctx, staying)
	f.setColumn(t, ctx, staying, conv.ToNullUUID(provider))

	f.remove(t, ctx, leavingMember)
	require.Equal(t, 0, f.bound(t, ctx, f.gatewayIssuerID, provider), "a stale column does not hold the binding")
}

func TestMemberProvider_AttachUsesTheAssociatedClientNotTheMemberIssuer(t *testing.T) {
	t.Parallel()
	ctx, f := newMemberProviderFixture(t)

	server := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	provider := seedRemoteSessionIssuer(t, ctx, f.ti.conn, f.projectID, f.organizationID, "member-provider")
	associated := createRemoteSessionClient(t, ctx, f.ti.conn, f.projectID, f.organizationID, provider, "associated-client")
	require.NoError(t, testrepo.New(f.ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{
		ProjectID:             f.projectID,
		OrganizationID:        f.organizationID,
		McpServerID:           server,
		RemoteSessionClientID: associated,
	}))

	f.add(t, ctx, server)

	rows, err := remotesessionsrepo.New(f.ti.conn).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: f.gatewayIssuerID,
		ProjectID:           conv.ToNullUUID(f.projectID),
		OrganizationID:      conv.ToPGText(f.organizationID),
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, associated, rows[0].ClientID)
}

func TestMemberProvider_ClientOnMemberIssuerWithoutAssociationIsNotAttached(t *testing.T) {
	t.Parallel()
	ctx, f := newMemberProviderFixture(t)

	server := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	provider := f.addProvider(t, ctx, server)
	other := createRemoteSessionClient(t, ctx, f.ti.conn, f.projectID, f.organizationID, provider, "unassociated-client")
	member, err := mcpserversrepo.New(f.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: server, ProjectID: f.projectID})
	require.NoError(t, err)
	require.NoError(t, remotesessionsrepo.New(f.ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: other,
		UserSessionIssuerID:   member.UserSessionIssuerID.UUID,
	}))

	f.add(t, ctx, server)

	rows, err := remotesessionsrepo.New(f.ti.conn).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: f.gatewayIssuerID,
		ProjectID:           conv.ToNullUUID(f.projectID),
		OrganizationID:      conv.ToPGText(f.organizationID),
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotEqual(t, other, rows[0].ClientID)
}

func TestMemberProvider_SeveralClientsOfOneProviderPreferTheMemberIssuer(t *testing.T) {
	t.Parallel()
	ctx, f := newMemberProviderFixture(t)

	server := seedMcpServer(t, ctx, f.ti.conn, f.projectID)
	provider := seedRemoteSessionIssuer(t, ctx, f.ti.conn, f.projectID, f.organizationID, "member-provider")
	older := createRemoteSessionClient(t, ctx, f.ti.conn, f.projectID, f.organizationID, provider, "older-client")
	require.NoError(t, testrepo.New(f.ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{
		ProjectID:             f.projectID,
		OrganizationID:        f.organizationID,
		McpServerID:           server,
		RemoteSessionClientID: older,
	}))
	own := createRemoteSessionClient(t, ctx, f.ti.conn, f.projectID, f.organizationID, provider, "own-client")
	stampAndWireMemberClient(t, ctx, f.ti.conn, f.projectID, f.organizationID, server, own)

	f.add(t, ctx, server)

	rows, err := remotesessionsrepo.New(f.ti.conn).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: f.gatewayIssuerID,
		ProjectID:           conv.ToNullUUID(f.projectID),
		OrganizationID:      conv.ToPGText(f.organizationID),
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, own, rows[0].ClientID)
}
