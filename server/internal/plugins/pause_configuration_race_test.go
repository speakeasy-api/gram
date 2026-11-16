package plugins_test

import (
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

// The pause must read generation configuration after waiting for an org-wide
// change, rather than reuse configuration read before the shared lock released.
func TestAutoSync_TurnOffSeesOrganizationChangeCommittedWhileWaiting(t *testing.T) {
	t.Parallel()
	ctx, f := newAutoSyncFixture(t, "configuration-race")
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	change := testenv.BeginTx(t, ctx, f.ti.conn)
	queries := orgrepo.New(change)
	before, err := queries.GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	domains := before.VerifiedDomains
	if domains == nil {
		domains = []string{}
	}
	_, err = queries.UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID: before.ID, Name: "Updated Configuration", WorkosID: before.WorkosID,
		WorkosUpdatedAt: before.WorkosUpdatedAt, WorkosLastEventID: before.WorkosLastEventID, VerifiedDomains: domains,
	})
	require.NoError(t, err)
	require.NoError(t, plugins.LockOrganizationPluginPackageContent(ctx, change, ac.ActiveOrganizationID))

	turnOff := make(chan error, 1)
	go func() {
		_, err := f.setAutoSync(ctx, f.paused, false)
		turnOff <- err
	}()
	waitRoleSetupBlocked(t, ctx, f.ti, int32(change.Conn().PgConn().PID()))
	require.NoError(t, change.Commit(ctx))
	select {
	case err := <-turnOff:
		require.ErrorIs(t, err, plugins.ErrPluginHasUnsyncedChanges, "the check must see configuration committed while it waited")
	case <-time.After(10 * time.Second):
		t.Fatal("turning auto sync off never finished")
	}
	plugin, err := f.ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: f.paused.ID})
	require.NoError(t, err)
	require.True(t, plugin.AutoSyncEnabled)
}
