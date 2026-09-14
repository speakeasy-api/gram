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
	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugin, err := pluginsrepo.New(ti.conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, admission.LockProject(ctx, tx, *authCtx.ProjectID))
	finished := make(chan error, 1)
	go func() {
		finished <- ti.service.DeletePlugin(ctx, &gen.DeletePluginPayload{ID: plugin.ID.String()})
	}()
	require.Never(t, func() bool { return len(finished) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.NoError(t, tx.Rollback(ctx))
	require.Eventually(t, func() bool { return len(finished) > 0 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, <-finished)
}
