package metamcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRemoveMetaMcpMember_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "removal host")
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	member, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerRemoveMember)
	require.NoError(t, err)

	require.NoError(t, ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               member.ID,
	}))

	result, err := ti.service.ListMetaMcpMembers(ctx, &gen.ListMetaMcpMembersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
	})
	require.NoError(t, err)
	require.Empty(t, result.Members)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerRemoveMember)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestRemoveMetaMcpMember_RepeatRemoveNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "double removal host")
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	member, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)

	require.NoError(t, ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               member.ID,
	}))

	err = ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               member.ID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRemoveMetaMcpMember_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               "not-a-uuid",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestRemoveMetaMcpMember_UnknownIDNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Removing a member reverses the add-side auto-attach: the member's provider
// client is unbound from the gateway issuer once no other live member fronts
// that upstream, so it stops appearing on the consent screen. While another
// member still fronts the same upstream, the binding is retained.
func TestRemoveMetaMcpMember_DetachesProviderClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	meta := seedMetaMcpServer(t, ctx, ti, "auto detach host")
	require.NotNil(t, meta.UserSessionIssuerID, "create mints the gateway issuer")
	gatewayIssuerID := uuid.MustParse(*meta.UserSessionIssuerID)

	remoteIssuerID := seedRemoteSessionIssuer(t, ctx, ti.conn, projectID, orgID, "auto-detach-rsi")

	addMember := func(serverID uuid.UUID) string {
		member, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			MetaMcpServerID:  meta.ID,
			McpServerID:      serverID.String(),
			SortOrder:        nil,
		})
		require.NoError(t, err)
		return member.ID
	}
	count := func() int {
		return countGatewayClientsForUpstream(t, ctx, ti.conn, projectID, orgID, gatewayIssuerID, remoteIssuerID)
	}

	// Two members of the same gateway fronting the same upstream AS; the "one
	// client per upstream per issuer" rule leaves exactly one binding.
	firstServer := seedMcpServer(t, ctx, ti.conn, projectID)
	stampAndWireMemberClient(t, ctx, ti.conn, projectID, firstServer, remoteIssuerID, createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, remoteIssuerID, "auto-detach-client-1"))
	firstMember := addMember(firstServer)

	secondServer := seedMcpServer(t, ctx, ti.conn, projectID)
	stampAndWireMemberClient(t, ctx, ti.conn, projectID, secondServer, remoteIssuerID, createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, remoteIssuerID, "auto-detach-client-2"))
	secondMember := addMember(secondServer)

	require.Equal(t, 1, count(), "one client per upstream per issuer")

	// Removing the first member leaves the second still fronting the upstream,
	// so the provider binding is retained.
	require.NoError(t, ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               firstMember,
	}))
	require.Equal(t, 1, count(), "binding retained while another member fronts the upstream")

	// Removing the last member fronting the upstream unbinds the provider.
	require.NoError(t, ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               secondMember,
	}))
	require.Equal(t, 0, count(), "binding detached once no member fronts the upstream")
}

// When two gateways share one user_session_issuer, the provider binding is
// scoped to that issuer, so removing a member from one gateway must NOT detach
// a provider the OTHER gateway still fronts.
func TestRemoveMetaMcpMember_RetainsProviderForSharedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	metaA := seedMetaMcpServer(t, ctx, ti, "shared issuer gateway A")
	require.NotNil(t, metaA.UserSessionIssuerID)
	sharedIssuerID := *metaA.UserSessionIssuerID
	gatewayIssuerID := uuid.MustParse(sharedIssuerID)

	// Gateway B reuses gateway A's issuer.
	metaB, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "shared issuer gateway B",
		UserSessionIssuerID: &sharedIssuerID,
		Visibility:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, sharedIssuerID, *metaB.UserSessionIssuerID, "gateway B shares gateway A's issuer")

	remoteIssuerID := seedRemoteSessionIssuer(t, ctx, ti.conn, projectID, orgID, "shared-rsi")

	addMember := func(metaID string, serverID uuid.UUID) string {
		member, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			MetaMcpServerID:  metaID,
			McpServerID:      serverID.String(),
			SortOrder:        nil,
		})
		require.NoError(t, err)
		return member.ID
	}
	count := func() int {
		return countGatewayClientsForUpstream(t, ctx, ti.conn, projectID, orgID, gatewayIssuerID, remoteIssuerID)
	}

	serverA := seedMcpServer(t, ctx, ti.conn, projectID)
	stampAndWireMemberClient(t, ctx, ti.conn, projectID, serverA, remoteIssuerID, createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, remoteIssuerID, "shared-client-a"))
	memberA := addMember(metaA.ID, serverA)

	serverB := seedMcpServer(t, ctx, ti.conn, projectID)
	stampAndWireMemberClient(t, ctx, ti.conn, projectID, serverB, remoteIssuerID, createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, remoteIssuerID, "shared-client-b"))
	addMember(metaB.ID, serverB)

	require.Equal(t, 1, count(), "one client per upstream on the shared issuer")

	// Removing gateway A's member must not strip the provider gateway B fronts.
	require.NoError(t, ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               memberA,
	}))
	require.Equal(t, 1, count(), "binding retained: gateway B still fronts the upstream on the shared issuer")
}
