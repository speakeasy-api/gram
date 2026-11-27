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

// Removing a member from one gateway does not change either server's offer,
// even when both gateways use the same downstream issuer.
func TestRemoveMetaMcpMember_PreservesOffersAcrossSharedIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *auth.ProjectID, auth.ActiveOrganizationID
	metaA := seedMetaMcpServer(t, ctx, ti, "shared issuer gateway A")
	require.NotNil(t, metaA.UserSessionIssuerID)
	metaB, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		Name: "shared issuer gateway B", UserSessionIssuerID: metaA.UserSessionIssuerID,
	})
	require.NoError(t, err)
	require.Equal(t, metaA.UserSessionIssuerID, metaB.UserSessionIssuerID)

	provider := seedRemoteSessionIssuer(t, ctx, ti.conn, projectID, orgID, "shared-provider")
	add := func(gatewayID, name string) (string, uuid.UUID, uuid.UUID) {
		server := seedMcpServer(t, ctx, ti.conn, projectID)
		registration := createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, provider, name)
		offer := seedMemberOffer(t, ctx, ti.conn, projectID, orgID, server, registration)
		member, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{MetaMcpServerID: gatewayID, McpServerID: server.String()})
		require.NoError(t, err)
		return member.ID, server, offer
	}
	memberA, serverA, offerA := add(metaA.ID, "registration-a")
	_, serverB, offerB := add(metaB.ID, "registration-b")

	require.NoError(t, ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{ID: memberA}))
	listed, err := ti.service.ListMetaMcpMembers(ctx, &gen.ListMetaMcpMembersPayload{MetaMcpServerID: metaA.ID})
	require.NoError(t, err)
	require.Empty(t, listed.Members)
	requireMemberOffers(t, ctx, ti, serverA, offerA)
	requireMemberOffers(t, ctx, ti, serverB, offerB)
}
