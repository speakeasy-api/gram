package mcpslugs_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_slugs"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetMcpSlug_ByID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()
	slugVal := authCtx.OrganizationSlug + "-example"

	created, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString(slugVal),
	})
	require.NoError(t, err)

	id := created.ID
	fetched, err := ti.service.GetMcpSlug(ctx, &gen.GetMcpSlugPayload{
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

func TestGetMcpSlug_BySlugOnPlatformDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()
	slugVal := authCtx.OrganizationSlug + "-by-slug-test"

	_, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString(slugVal),
	})
	require.NoError(t, err)

	slugParam := types.McpSlugString(slugVal)
	fetched, err := ti.service.GetMcpSlug(ctx, &gen.GetMcpSlugPayload{
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

func TestGetMcpSlug_NeitherIDNorSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetMcpSlug(ctx, &gen.GetMcpSlugPayload{
		ID:               nil,
		CustomDomainID:   nil,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestGetMcpSlug_BothIDAndSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	slug := types.McpSlugString("somewhere-example")
	_, err := ti.service.GetMcpSlug(ctx, &gen.GetMcpSlugPayload{
		ID:               &id,
		CustomDomainID:   nil,
		Slug:             &slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestGetMcpSlug_IDWithCustomDomainRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	customDomain := uuid.NewString()
	_, err := ti.service.GetMcpSlug(ctx, &gen.GetMcpSlugPayload{
		ID:               &id,
		CustomDomainID:   &customDomain,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestGetMcpSlug_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	_, err := ti.service.GetMcpSlug(ctx, &gen.GetMcpSlugPayload{
		ID:               &id,
		CustomDomainID:   nil,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetMcpSlug_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	id := uuid.NewString()
	_, err := ti.service.GetMcpSlug(ctx, &gen.GetMcpSlugPayload{
		ID:               &id,
		CustomDomainID:   nil,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
