package plugins_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestEnsureDefaultPlugin_CreatesWhenMissing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	result, err := plugins.EnsureDefaultPlugin(ctx, queries, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, "Default", result.Plugin.Name)
	require.Equal(t, "default", result.Plugin.Slug)
	require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, result.Plugin.IsDefault)
}

func TestEnsureDefaultPlugin_ReturnsExistingWhenPresent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	first, err := plugins.EnsureDefaultPlugin(ctx, queries, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.NoError(t, err)
	require.True(t, first.Created)

	second, err := plugins.EnsureDefaultPlugin(ctx, queries, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.NoError(t, err)
	require.False(t, second.Created)
	require.Equal(t, first.Plugin.ID, second.Plugin.ID)
}

func TestEnsureDefaultPlugin_ConflictWithExistingNonDefaultPlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	// A plugin already occupies the "Default"/"default" name+slug, but isn't
	// marked is_default — a real conflict, not a race with another Ensure
	// call, so it must surface as an error rather than being masked.
	_, err := queries.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Default",
		Slug:           "default",
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)

	_, err = plugins.EnsureDefaultPlugin(ctx, queries, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.Error(t, err)
}
