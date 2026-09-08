package metamcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListMetaMcpMembers_OrdersBySortOrder(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "ordered host")
	firstServer := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	secondServer := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	// Insert with descending sort orders so listing has to reorder.
	_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      firstServer.String(),
		SortOrder:        conv.PtrEmpty(2),
	})
	require.NoError(t, err)
	_, err = ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      secondServer.String(),
		SortOrder:        conv.PtrEmpty(1),
	})
	require.NoError(t, err)

	result, err := ti.service.ListMetaMcpMembers(ctx, &gen.ListMetaMcpMembersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
	})
	require.NoError(t, err)
	require.Len(t, result.Members, 2)
	require.Equal(t, secondServer.String(), result.Members[0].McpServerID)
	require.Equal(t, firstServer.String(), result.Members[1].McpServerID)
}

func TestListMetaMcpMembers_OmitsDeletedServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "pruned host")
	keptServer := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	doomedServer := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	for _, serverID := range []uuid.UUID{keptServer, doomedServer} {
		_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			MetaMcpServerID:  meta.ID,
			McpServerID:      serverID.String(),
			SortOrder:        nil,
		})
		require.NoError(t, err)
	}

	// Tombstone one member server directly at the repo layer. The listing must
	// hide its membership row even though the row itself is still live.
	_, err := mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{
		ID:        doomedServer,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	result, err := ti.service.ListMetaMcpMembers(ctx, &gen.ListMetaMcpMembersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
	})
	require.NoError(t, err)
	require.Len(t, result.Members, 1)
	require.Equal(t, keptServer.String(), result.Members[0].McpServerID)
}

func TestListMetaMcpMembers_UnknownMetaNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.ListMetaMcpMembers(ctx, &gen.ListMetaMcpMembersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListMetaMcpMembers_CrossProjectMetaInvisible(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherProjectID := seedOtherProject(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	foreignMeta, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           otherProjectID,
		Name:                "foreign meta",
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:          visibility.Private,
	})
	require.NoError(t, err)

	_, err = ti.service.ListMetaMcpMembers(ctx, &gen.ListMetaMcpMembersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  foreignMeta.ID.String(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListMetaMcpMembers_RequiresReadScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListMetaMcpMembers(ctx, &gen.ListMetaMcpMembersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
