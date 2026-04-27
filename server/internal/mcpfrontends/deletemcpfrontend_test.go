package mcpfrontends_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_frontends"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpslugsrepo "github.com/speakeasy-api/gram/server/internal/mcpslugs/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteMcpFrontend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpFrontend(ctx, &gen.CreateMcpFrontendPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     &serverID,
		ToolsetID:             nil,
		Visibility:            types.McpFrontendVisibility("disabled"),
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpFrontendDelete)
	require.NoError(t, err)

	err = ti.service.DeleteMcpFrontend(ctx, &gen.DeleteMcpFrontendPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpFrontendDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Confirm subsequent get returns not-found.
	_, err = ti.service.GetMcpFrontend(ctx, &gen.GetMcpFrontendPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteMcpFrontend_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.DeleteMcpFrontend(ctx, &gen.DeleteMcpFrontendPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteMcpFrontend_CascadesSoftDeleteToSlugs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Create a frontend and two slugs that both point at it.
	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	frontend, err := ti.service.CreateMcpFrontend(ctx, &gen.CreateMcpFrontendPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     &serverID,
		ToolsetID:             nil,
		Visibility:            types.McpFrontendVisibility("disabled"),
	})
	require.NoError(t, err)

	frontendUUID := uuid.MustParse(frontend.ID)
	slugRepo := mcpslugsrepo.New(ti.conn)
	for _, v := range []string{"-one", "-two"} {
		_, err := slugRepo.CreateMCPSlug(ctx, mcpslugsrepo.CreateMCPSlugParams{
			ProjectID:      *authCtx.ProjectID,
			CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			McpFrontendID:  frontendUUID,
			Slug:           authCtx.OrganizationSlug + v,
		})
		require.NoError(t, err)
	}

	// Delete the frontend.
	err = ti.service.DeleteMcpFrontend(ctx, &gen.DeleteMcpFrontendPayload{
		ID:               frontend.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// Both child slugs must now be absent from the active set.
	remaining, err := slugRepo.ListMCPSlugsByProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	for _, s := range remaining {
		require.NotEqual(t, frontendUUID, s.McpFrontendID, "slug pointing at deleted frontend should have been soft-deleted")
	}
}

func TestDeleteMcpFrontend_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	err := ti.service.DeleteMcpFrontend(ctx, &gen.DeleteMcpFrontendPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
