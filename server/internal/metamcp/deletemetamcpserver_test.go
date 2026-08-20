package metamcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteMetaMcpServer_TombstonesMembershipsAndEndpoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "doomed gateway",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)

	memberServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	member, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  created.ID,
		McpServerID:      memberServerID.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)

	metaUUID := uuid.MustParse(created.ID)
	endpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		MetaMcpServerID: uuid.NullUUID{UUID: metaUUID, Valid: true},
		Slug:            authCtx.OrganizationSlug + "-meta-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	deleteBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerDelete)
	require.NoError(t, err)
	removeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerRemoveMember)
	require.NoError(t, err)
	endpointDeleteBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)

	err = ti.service.DeleteMetaMcpServer(ctx, &gen.DeleteMetaMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
	})
	require.NoError(t, err)

	_, err = ti.service.GetMetaMcpServer(ctx, &gen.GetMetaMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	memberships, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMetaMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMetaMCPServerIDParams{
		ProjectID:       *authCtx.ProjectID,
		MetaMcpServerID: metaUUID,
	})
	require.NoError(t, err)
	require.Empty(t, memberships, "endpoint %s should have been tombstoned with membership %s", endpoint.ID, member.ID)

	deleteAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerDelete)
	require.NoError(t, err)
	require.Equal(t, deleteBefore+1, deleteAfter)

	removeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerRemoveMember)
	require.NoError(t, err)
	require.Equal(t, removeBefore+1, removeAfter, "each cascaded membership must produce its own remove event")

	endpointDeleteAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	require.Equal(t, endpointDeleteBefore+1, endpointDeleteAfter, "each cascaded endpoint must produce its own delete event")
}

func TestDeleteMetaMcpServer_RepeatDeleteNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "double delete",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteMetaMcpServer(ctx, &gen.DeleteMetaMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
	}))

	err = ti.service.DeleteMetaMcpServer(ctx, &gen.DeleteMetaMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
