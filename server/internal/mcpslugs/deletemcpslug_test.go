package mcpslugs_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_slugs"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteMcpSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString(authCtx.OrganizationSlug + "-delete-me"),
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpSlugDelete)
	require.NoError(t, err)

	err = ti.service.DeleteMcpSlug(ctx, &gen.DeleteMcpSlugPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpSlugDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Subsequent get returns not-found.
	id := created.ID
	_, err = ti.service.GetMcpSlug(ctx, &gen.GetMcpSlugPayload{
		ID:               &id,
		CustomDomainID:   nil,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteMcpSlug_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.DeleteMcpSlug(ctx, &gen.DeleteMcpSlugPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteMcpSlug_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	err := ti.service.DeleteMcpSlug(ctx, &gen.DeleteMcpSlugPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
