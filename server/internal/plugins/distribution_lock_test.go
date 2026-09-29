package plugins_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDeleteDefaultPluginWaitsForAdmissionLock(t *testing.T) {
	t.Parallel()
	// probeTimeout bounds the server lock probe.
	const probeTimeout = 100 * time.Millisecond
	ctx, ti := newTestPluginsService(t, probeTimeout)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugin, err := pluginsrepo.New(ti.conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, admission.LockProject(ctx, tx, *authCtx.ProjectID))
	testenv.RequireLockNotAvailable(t, ti.service.DeletePlugin(ctx, &gen.DeletePluginPayload{ID: plugin.ID.String()}))
	require.NoError(t, tx.Rollback(ctx))
	require.NoError(t, ti.service.DeletePlugin(ctx, &gen.DeletePluginPayload{ID: plugin.ID.String()}))
}
