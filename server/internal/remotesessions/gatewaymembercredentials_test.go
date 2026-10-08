package remotesessions_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// gatewayAttachFixture is a gateway on its own user session issuer whose two
// members share one remote issuer through distinct clients. Only the first
// member's client is bound to the gateway issuer.
type gatewayAttachFixture struct {
	ti              *testInstance
	flags           *feature.InMemory
	orgID           string
	projectID       uuid.UUID
	remoteIssuerID  string
	gatewayIssuerID uuid.UUID
	firstClient     string
	secondClient    string
}

func newGatewayAttachFixture(t *testing.T, ctx context.Context, ti *testInstance, slug string) *gatewayAttachFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	flags := &feature.InMemory{}
	ti.service.SetFeatureFlags(flags)

	remoteIssuerID := createRemoteIssuer(t, ctx, ti, slug+"-rsi", "")
	// Gateways reference their issuer by (organization, id), so the issuer
	// must carry the organization.
	gatewayIssuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		OrganizationID:     conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:               slug + "-gw",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	gatewayIssuerID := gatewayIssuer.ID
	gateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Name:                slug + " gateway",
		UserSessionIssuerID: conv.ToNullUUID(gatewayIssuerID),
		Visibility:          "private",
		NetworkAccessMode:   pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	addMember := func(memberSlug, clientID string) string {
		memberIssuerID := createUserSessionIssuer(t, ctx, ti.conn, memberSlug+"-usi")
		client := createRemoteClient(t, ctx, ti, remoteIssuerID, memberIssuerID.String(), clientID)
		serverID := seedGatewayMemberServer(t, ctx, ti, memberIssuerID, uuid.MustParse(remoteIssuerID), memberSlug)
		_, err := metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
			ProjectID:       *authCtx.ProjectID,
			MetaMcpServerID: gateway.ID,
			McpServerID:     serverID,
			SortOrder:       0,
		})
		require.NoError(t, err)
		return client
	}

	f := &gatewayAttachFixture{
		ti:              ti,
		flags:           flags,
		orgID:           authCtx.ActiveOrganizationID,
		projectID:       *authCtx.ProjectID,
		remoteIssuerID:  remoteIssuerID,
		gatewayIssuerID: gatewayIssuerID,
		firstClient:     addMember(slug+"-first", slug+"-client-1"),
		secondClient:    addMember(slug+"-second", slug+"-client-2"),
	}
	require.NoError(t, f.attach(ctx, f.firstClient))
	return f
}

func (f *gatewayAttachFixture) setEnabled(enabled bool) {
	f.flags.SetFlag(feature.FlagGatewayMemberCredentials, f.orgID, enabled)
}

func (f *gatewayAttachFixture) attach(ctx context.Context, clientID string) error {
	_, err := f.ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{
		ID:                  clientID,
		UserSessionIssuerID: f.gatewayIssuerID.String(),
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	if err != nil {
		return fmt.Errorf("attach client to gateway issuer: %w", err)
	}
	return nil
}

func (f *gatewayAttachFixture) gatewayClients(t *testing.T, ctx context.Context) int {
	t.Helper()
	rows, err := repo.New(f.ti.conn).ListRemoteSessionClientsForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: f.gatewayIssuerID,
		ProjectID:           conv.ToNullUUID(f.projectID),
		OrganizationID:      conv.ToPGText(f.orgID),
	})
	require.NoError(t, err)
	return len(rows)
}

// seedGatewayMemberServer creates a remote MCP server on the member's own user
// session issuer, stamped with the remote issuer it authenticates against.
func seedGatewayMemberServer(t *testing.T, ctx context.Context, ti *testInstance, memberIssuerID, remoteIssuerID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteServer, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     *authCtx.ProjectID,
		TransportType: "sse",
		Url:           "https://" + slug + ".example.com/mcp",
	})
	require.NoError(t, err)

	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remoteServer.ID),
		Visibility:          "private",
		UserSessionIssuerID: conv.ToNullUUID(memberIssuerID),
	})
	require.NoError(t, err)

	stamped, err := testrepo.New(ti.conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		RemoteSessionIssuerID: conv.ToNullUUID(remoteIssuerID),
		ID:                    server.ID,
		ProjectID:             *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stamped)
	return server.ID
}

func TestAttachUserSessionIssuer_GatewayMemberClientNeedsRollout(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	f := newGatewayAttachFixture(t, ctx, ti, "gw-attach-off")

	requireOopsCode(t, f.attach(ctx, f.secondClient), oops.CodeConflict)
	require.Equal(t, 1, f.gatewayClients(t, ctx))
}

func TestAttachUserSessionIssuer_GatewayMemberClientWithRollout(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	f := newGatewayAttachFixture(t, ctx, ti, "gw-attach-on")
	f.setEnabled(true)

	require.NoError(t, f.attach(ctx, f.secondClient))
	require.Equal(t, 2, f.gatewayClients(t, ctx), "each member's client is bound to the gateway issuer")

	// Re-attaching either client stays a no-op.
	require.NoError(t, f.attach(ctx, f.firstClient))
	require.Equal(t, 2, f.gatewayClients(t, ctx))
}

func TestAttachUserSessionIssuer_GatewayRejectsUnassociatedClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	f := newGatewayAttachFixture(t, ctx, ti, "gw-attach-stray")
	f.setEnabled(true)

	// Same remote issuer, but no gateway member is configured with it.
	stray, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: f.remoteIssuerID,
		UserSessionIssuerIds:  nil,
		ClientID:              "gw-attach-stray-client",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	requireOopsCode(t, f.attach(ctx, stray.ID), oops.CodeConflict)
	require.Equal(t, 1, f.gatewayClients(t, ctx))
}

func TestAttachUserSessionIssuer_GatewayRejectsMemberClientBesideStray(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	f := newGatewayAttachFixture(t, ctx, ti, "gw-attach-beside")

	// Before the rollout an administrator replaced the member's client with
	// one no gateway member is configured with; alone, it is allowed.
	_, err := ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{
		ID:                  f.firstClient,
		UserSessionIssuerID: f.gatewayIssuerID.String(),
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	stray, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: f.remoteIssuerID,
		UserSessionIssuerIds:  nil,
		ClientID:              "gw-attach-beside-stray",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.NoError(t, f.attach(ctx, stray.ID))

	// A member's own client cannot join it: the stray would share the
	// per-member credentials issuer without belonging to any member.
	f.setEnabled(true)
	requireOopsCode(t, f.attach(ctx, f.secondClient), oops.CodeConflict)
	require.Equal(t, 1, f.gatewayClients(t, ctx))
}

func TestAttachUserSessionIssuer_GatewayRejectsSharedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	f := newGatewayAttachFixture(t, ctx, ti, "gw-attach-shared")
	f.setEnabled(true)

	// A direct server on the gateway's issuer makes it shared.
	seedRemoteMCPServerForIssuer(t, ctx, ti, f.gatewayIssuerID, "gw-attach-shared-direct", "https://gw-attach-shared-direct.example.com/mcp")

	requireOopsCode(t, f.attach(ctx, f.secondClient), oops.CodeConflict)
	require.Equal(t, 1, f.gatewayClients(t, ctx))
}
