package metamcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metamcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestCreateMetaMcpServer_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "my gateway",
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.ID)
	require.Equal(t, "my gateway", result.Name)
	require.Equal(t, authCtx.ActiveOrganizationID, result.OrganizationID)
	require.Equal(t, authCtx.ProjectID.String(), result.ProjectID)
	require.Nil(t, result.UserSessionIssuerID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateMetaMcpServer_WithIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)

	result, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "issuer gateway",
		UserSessionIssuerID: conv.PtrEmpty(issuerID.String()),
	})
	require.NoError(t, err)
	require.NotNil(t, result.UserSessionIssuerID)
	require.Equal(t, issuerID.String(), *result.UserSessionIssuerID)
}

func TestCreateMetaMcpServer_RejectsForeignProjectIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherProjectID := seedOtherProject(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	foreignIssuerID := seedUserSessionIssuer(t, ctx, ti.conn, otherProjectID)

	_, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "issuer gateway",
		UserSessionIssuerID: conv.PtrEmpty(foreignIssuerID.String()),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMetaMcpServer_RejectsDeletedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := seedUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	_, err := usersessionsrepo.New(ti.conn).DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{
		ID:        issuerID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	_, err = ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "issuer gateway",
		UserSessionIssuerID: conv.PtrEmpty(issuerID.String()),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMetaMcpServer_RequiresWriteScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, authz.WildcardResource))

	_, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "denied gateway",
		UserSessionIssuerID: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateMetaMcpServer_DefaultsToPrivate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "default visibility gateway",
		UserSessionIssuerID: nil,
		Visibility:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, types.MetaMcpServerVisibility(metamcp.VisibilityPrivate), result.Visibility)
}

func TestCreateMetaMcpServer_AcceptsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	disabled := types.MetaMcpServerVisibility(metamcp.VisibilityDisabled)
	result, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "disabled gateway",
		UserSessionIssuerID: nil,
		Visibility:          &disabled,
	})
	require.NoError(t, err)
	require.Equal(t, disabled, result.Visibility)
}
