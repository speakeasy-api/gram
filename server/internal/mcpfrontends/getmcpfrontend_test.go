package mcpfrontends_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_frontends"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetMcpFrontend(t *testing.T) {
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
		Visibility:            types.McpFrontendVisibility("private"),
	})
	require.NoError(t, err)

	fetched, err := ti.service.GetMcpFrontend(ctx, &gen.GetMcpFrontendPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	require.Equal(t, types.McpFrontendVisibility("private"), fetched.Visibility)
}

func TestGetMcpFrontend_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetMcpFrontend(ctx, &gen.GetMcpFrontendPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetMcpFrontend_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetMcpFrontend(ctx, &gen.GetMcpFrontendPayload{
		ID:               "not-a-uuid",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetMcpFrontend_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.GetMcpFrontend(ctx, &gen.GetMcpFrontendPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
