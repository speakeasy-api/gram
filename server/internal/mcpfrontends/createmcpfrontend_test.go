package mcpfrontends_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_frontends"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateMcpFrontend_RemoteMcpBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpFrontendCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateMcpFrontend(ctx, &gen.CreateMcpFrontendPayload{
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
	require.NotNil(t, result)

	require.NotEmpty(t, result.ID)
	require.NotEmpty(t, result.ProjectID)
	require.NotNil(t, result.RemoteMcpServerID)
	require.Equal(t, serverID, *result.RemoteMcpServerID)
	require.Nil(t, result.ToolsetID)
	require.Nil(t, result.ExternalOauthServerID)
	require.Nil(t, result.OauthProxyServerID)
	require.Equal(t, types.McpFrontendVisibility("disabled"), result.Visibility)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpFrontendCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateMcpFrontend_MissingBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateMcpFrontend(ctx, &gen.CreateMcpFrontendPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     nil,
		ToolsetID:             nil,
		Visibility:            types.McpFrontendVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpFrontend_BothBackends(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	toolsetID := "00000000-0000-0000-0000-000000000001"

	_, err := ti.service.CreateMcpFrontend(ctx, &gen.CreateMcpFrontendPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		EnvironmentID:         nil,
		ExternalOauthServerID: nil,
		OauthProxyServerID:    nil,
		RemoteMcpServerID:     &serverID,
		ToolsetID:             &toolsetID,
		Visibility:            types.McpFrontendVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpFrontend_BothOAuthSources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	extOAuthID := "00000000-0000-0000-0000-000000000002"
	proxyOAuthID := "00000000-0000-0000-0000-000000000003"

	_, err := ti.service.CreateMcpFrontend(ctx, &gen.CreateMcpFrontendPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		EnvironmentID:         nil,
		ExternalOauthServerID: &extOAuthID,
		OauthProxyServerID:    &proxyOAuthID,
		RemoteMcpServerID:     &serverID,
		ToolsetID:             nil,
		Visibility:            types.McpFrontendVisibility("disabled"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpFrontend_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	// Grant only read, attempt create (requires write).
	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.CreateMcpFrontend(ctx, &gen.CreateMcpFrontendPayload{
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
	requireOopsCode(t, err, oops.CodeForbidden)
}
