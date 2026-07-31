package unproxiedmcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/unproxied_mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListTools_UnreachableServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	staffCtx := withStaffEmail(t, ctx)

	created, err := ti.service.CreateServer(staffCtx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             nil,
		URL:              "https://vendor.invalid/mcp",
		Description:      nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "unreachable", result.Status)
	require.Empty(t, result.Tools)
	require.NotNil(t, result.Message)
}

func TestListTools_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		ID:               "00000000-0000-0000-0000-000000000000",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
