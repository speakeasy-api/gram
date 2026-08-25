package metamcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
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

	result, err := ti.service.ListMetaMcpServers(ctx, &gen.ListMetaMcpServersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	ids := make([]string, 0, len(result.MetaMcpServers))
	for _, s := range result.MetaMcpServers {
		ids = append(ids, s.ID)
	}
	require.Contains(t, ids, first.ID)
	require.Contains(t, ids, second.ID)
}
