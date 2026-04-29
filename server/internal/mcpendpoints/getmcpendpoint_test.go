package mcpendpoints_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetMcpEndpoint_ByID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	slugVal := authCtx.OrganizationSlug + "-example"

	created, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slugVal),
	})
	require.NoError(t, err)

	id := created.ID
	fetched, err := ti.service.GetMcpEndpoint(ctx, &gen.GetMcpEndpointPayload{
		ID:               &id,
		CustomDomainID:   nil,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	require.Equal(t, slugVal, string(fetched.Slug))
}

func TestGetMcpEndpoint_BySlugOnPlatformDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	slugVal := authCtx.OrganizationSlug + "-by-slug-test"

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slugVal),
	})
	require.NoError(t, err)

	slugParam := types.McpEndpointSlug(slugVal)
	fetched, err := ti.service.GetMcpEndpoint(ctx, &gen.GetMcpEndpointPayload{
		ID:               nil,
		CustomDomainID:   nil,
		Slug:             &slugParam,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, slugVal, string(fetched.Slug))
	require.Nil(t, fetched.CustomDomainID)
}

func TestGetMcpEndpoint_NeitherIDNorSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetMcpEndpoint(ctx, &gen.GetMcpEndpointPayload{
		ID:               nil,
		CustomDomainID:   nil,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestGetMcpEndpoint_BothIDAndSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	slug := types.McpEndpointSlug("somewhere-example")
	_, err := ti.service.GetMcpEndpoint(ctx, &gen.GetMcpEndpointPayload{
		ID:               &id,
		CustomDomainID:   nil,
		Slug:             &slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestGetMcpEndpoint_IDWithCustomDomainRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	customDomain := uuid.NewString()
	_, err := ti.service.GetMcpEndpoint(ctx, &gen.GetMcpEndpointPayload{
		ID:               &id,
		CustomDomainID:   &customDomain,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestGetMcpEndpoint_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	_, err := ti.service.GetMcpEndpoint(ctx, &gen.GetMcpEndpointPayload{
		ID:               &id,
		CustomDomainID:   nil,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetMcpEndpoint_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	id := uuid.NewString()
	_, err := ti.service.GetMcpEndpoint(ctx, &gen.GetMcpEndpointPayload{
		ID:               &id,
		CustomDomainID:   nil,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
