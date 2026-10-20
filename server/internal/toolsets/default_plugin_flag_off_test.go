package toolsets_test

// Phase A expectations from main: with the server-key rollout off, Default plugin attaches keep the toolset key.

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestToolsetsService_CreateToolset_AttachesToDefaultPluginFlagOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ti.service.WithFeatureProvider(serverKeyFlagsOff())

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginsQueries := pluginsrepo.New(ti.conn)
	defaultPlugin, err := pluginsQueries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)

	// First toolset in a fresh org auto-enables MCP.
	result, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Attach Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.True(t, *result.McpEnabled)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, uuid.MustParse(result.ID), servers[0].ToolsetID.UUID)
	require.Equal(t, "Attach Toolset", servers[0].DisplayName)
	require.Equal(t, "required", servers[0].Policy)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_CreateToolset_SecondToolset_DoesNotAutoAttachFlagOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ti.service.WithFeatureProvider(serverKeyFlagsOff())

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginsQueries := pluginsrepo.New(ti.conn)
	defaultPlugin, err := pluginsQueries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	_, err = ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "First Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	second, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Second Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.False(t, *second.McpEnabled)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1, "only the auto-enabled first toolset should be attached")
	require.NotEqual(t, uuid.MustParse(second.ID), servers[0].ToolsetID.UUID)
}

func TestToolsetsService_UpdateToolset_EnableMcp_AttachesToDefaultPluginFlagOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ti.service.WithFeatureProvider(serverKeyFlagsOff())

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginsQueries := pluginsrepo.New(ti.conn)
	defaultPlugin, err := pluginsQueries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	// First toolset in the org auto-enables MCP (and gets attached).
	_, err = ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "First Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Second toolset in the org does not auto-enable.
	second, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Second Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.False(t, *second.McpEnabled)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1, "only the auto-enabled first toolset should be attached so far")

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)

	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   second.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpIsPublic:            nil,
		McpEnabled:             new(true),
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.True(t, *result.McpEnabled)

	servers, err = pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 2)
	for _, server := range servers {
		require.False(t, server.McpServerID.Valid, "re-enable keeps writing the toolset key until readers on every pod handle server keys")
	}
	require.Contains(t, []uuid.UUID{servers[0].ToolsetID.UUID, servers[1].ToolsetID.UUID}, uuid.MustParse(second.ID))

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Idempotent: re-enabling an already-enabled toolset must not double-attach.
	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   second.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpIsPublic:            nil,
		McpEnabled:             new(true),
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	servers, err = pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 2, "re-enabling an already-enabled toolset must not double-attach")
}
