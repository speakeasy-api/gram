package plugins_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestOrganizationPublicationWaitsForPluginPauseCheck(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("emission_%t", enabled), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			pause, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: exercise transaction lock ordering
			require.NoError(t, err)
			defer func() { _ = pause.Rollback(context.WithoutCancel(ctx)) }()
			require.NoError(t, pluginsrepo.New(pause).LockPluginPackageContent(ctx, ac.ProjectID.String()))

			done := make(chan error, 1)
			go func() {
				tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: concurrent organization mutation transaction
				if err != nil {
					done <- err
					return
				}
				defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
				if err := (plugins.PublicationRequests{Enabled: enabled}).Organization(ctx, tx, ac.ActiveOrganizationID, ac.UserID); err != nil {
					done <- err
					return
				}
				done <- tx.Commit(ctx)
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := ti.conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND wait_event = 'advisory' AND query LIKE '%plugin-package-content:%')`).Scan(&waiting) //nolint:glint // notestingrawsql: prove organization mutation reached its content lock
				return err == nil && waiting
			}, 10*time.Second, 10*time.Millisecond)
			select {
			case err := <-done:
				t.Fatalf("organization mutation finished during pause check: %v", err)
			default:
			}
			require.NoError(t, pause.Commit(ctx))
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("organization mutation remained blocked after pause check committed")
			}
		})
	}
}
