package plugins_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestPluginsService_SetDefaultPlugin_MovesDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	// ListPlugins lazily provisions the reserved "default" plugin, mirroring
	// how a project acquires its initial default routing target.
	_, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)

	original, err := queries.GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	target, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Moonpay MCP servers"})
	require.NoError(t, err)
	require.False(t, target.IsDefault)

	result, err := ti.service.SetDefaultPlugin(ctx, &gen.SetDefaultPluginPayload{ID: target.ID})
	require.NoError(t, err)
	require.True(t, result.IsDefault)
	require.Equal(t, target.ID, result.ID)

	// The chosen plugin is now the routing target, and the previous default
	// has been cleared so the single-default invariant still holds.
	def, err := queries.GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, target.ID, def.ID.String())

	previous, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: original.ID.String()})
	require.NoError(t, err)
	require.False(t, previous.IsDefault)
}

func TestPluginsService_SetDefaultPlugin_Idempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	target, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Idempotent Default"})
	require.NoError(t, err)

	first, err := ti.service.SetDefaultPlugin(ctx, &gen.SetDefaultPluginPayload{ID: target.ID})
	require.NoError(t, err)
	require.True(t, first.IsDefault)

	// Re-setting the already-default plugin is a no-op, not an error.
	second, err := ti.service.SetDefaultPlugin(ctx, &gen.SetDefaultPluginPayload{ID: target.ID})
	require.NoError(t, err)
	require.True(t, second.IsDefault)
	require.Equal(t, target.ID, second.ID)
}

func TestPluginsService_SetDefaultPlugin_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, err := ti.service.SetDefaultPlugin(ctx, &gen.SetDefaultPluginPayload{ID: uuid.New().String()})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestPluginsService_SetDefaultPlugin_ForbiddenWithoutOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	target, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Forbidden Default"})
	require.NoError(t, err)

	ctx = authz.GrantsToContext(ctx, nil)

	_, err = ti.service.SetDefaultPlugin(ctx, &gen.SetDefaultPluginPayload{ID: target.ID})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
