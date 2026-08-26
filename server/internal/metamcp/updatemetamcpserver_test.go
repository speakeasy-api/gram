package metamcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metamcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpdateMetaMcpServer_RenamesAndAttachesIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "before rename",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)

	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                "after rename",
		UserSessionIssuerID: conv.PtrEmpty(issuerID.String()),
	})
	require.NoError(t, err)
	require.Equal(t, "after rename", updated.Name)
	require.NotNil(t, updated.UserSessionIssuerID)
	require.Equal(t, issuerID.String(), *updated.UserSessionIssuerID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestUpdateMetaMcpServer_OmittedIssuerClearsReference(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "issuer holder",
		UserSessionIssuerID: conv.PtrEmpty(issuerID.String()),
	})
	require.NoError(t, err)
	require.NotNil(t, created.UserSessionIssuerID)

	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                "issuer holder",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)
	require.Nil(t, updated.UserSessionIssuerID)
}

func TestUpdateMetaMcpServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  uuid.NewString(),
		Name:                "ghost",
		UserSessionIssuerID: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateMetaMcpServer_OmittedVisibilityIsPreserved(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	disabled := types.MetaMcpServerVisibility(metamcp.VisibilityDisabled)
	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "stays disabled",
		UserSessionIssuerID: nil,
		Visibility:          &disabled,
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                "renamed while disabled",
		UserSessionIssuerID: nil,
		Visibility:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, "renamed while disabled", updated.Name)
	require.Equal(t, disabled, updated.Visibility)
}

func TestUpdateMetaMcpServer_ChangesVisibility(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "to disable",
		UserSessionIssuerID: nil,
		Visibility:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, types.MetaMcpServerVisibility(metamcp.VisibilityPrivate), created.Visibility)

	disabled := types.MetaMcpServerVisibility(metamcp.VisibilityDisabled)
	updated, err := ti.service.UpdateMetaMcpServer(ctx, &gen.UpdateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ID:                  created.ID,
		Name:                created.Name,
		UserSessionIssuerID: nil,
		Visibility:          &disabled,
	})
	require.NoError(t, err)
	require.Equal(t, disabled, updated.Visibility)
}
