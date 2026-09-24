package metamcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetMetaMcpServer_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "gettable gateway",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)

	got, err := ti.service.GetMetaMcpServer(ctx, &gen.GetMetaMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "gettable gateway", got.Name)
}

func TestGetMetaMcpServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetMetaMcpServer(ctx, &gen.GetMetaMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetMetaMcpServer_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetMetaMcpServer(ctx, &gen.GetMetaMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               "not-a-uuid",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListMetaMcpServers_ReturnsProjectServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	first, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "gateway one",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)

	second, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "gateway two",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	_, err = ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  first.ID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListMetaMcpServers(ctx, &gen.ListMetaMcpServersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	countByID := make(map[string]*int, len(result.MetaMcpServers))
	for _, s := range result.MetaMcpServers {
		countByID[s.ID] = s.MemberCount
	}
	require.Contains(t, countByID, first.ID)
	require.Contains(t, countByID, second.ID)
	require.NotNil(t, countByID[first.ID])
	require.Equal(t, 1, *countByID[first.ID])
	require.NotNil(t, countByID[second.ID])
	require.Equal(t, 0, *countByID[second.ID])
}
