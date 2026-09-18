package identityproviderconnections_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	apprepo "github.com/speakeasy-api/gram/server/internal/oktaapplications/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

func TestApplicationsSync_WaitingApplyReadsCommittedWatermark(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)

	setFixtures(si, []okta.App{fixtureApp(appA, "Current app", "oidc_client")}, nil, nil)
	runSync(t, ctx, syncer, id)

	// Hold the connection row while an older apply starts. NO KEY UPDATE
	// allows its run-row FK check through, but blocks LockSyncConnection's
	// FOR UPDATE exactly as another apply holding that row would.
	newer, err := si.conn.conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = newer.Rollback(context.Background()) }()
	_, err = newer.Exec(ctx, "SELECT id FROM identity_provider_connections WHERE id = $1 FOR NO KEY UPDATE", id)
	require.NoError(t, err)
	blockerPID := newer.Conn().PgConn().PID()

	setFixtures(si, []okta.App{fixtureApp(appB, "Stale app", "oidc_client")}, nil, nil)
	done := make(chan error, 1)
	go func() { done <- syncer.Run(ctx, id, false) }()

	// Observe the actual lock wait, not a sleep or merely goroutine startup.
	// The watermark must still be old when the lock statement takes its
	// snapshot; only then do we commit the newer watermark and release it.
	require.Eventually(t, func() bool {
		var waiting bool
		err := si.conn.conn.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database()
				  AND query LIKE '-- name: LockSyncConnection%'
				  AND wait_event_type = 'Lock'
				  AND $1::integer = ANY(pg_blocking_pids(pid))
			)`, blockerPID).Scan(&waiting)
		require.NoError(t, err)
		return waiting
	}, 5*time.Second, 10*time.Millisecond)

	watermark := pgtype.Timestamptz{Time: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond), Valid: true}
	changed, err := apprepo.New(newer).MarkApplicationsSynced(ctx, apprepo.MarkApplicationsSyncedParams{
		SyncedAt:                     watermark,
		IdentityProviderConnectionID: id,
		OrganizationID:               si.orgID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, changed)
	require.NoError(t, newer.Commit(ctx))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	run := latestRun(t, ctx, si, id)
	require.Equal(t, "failed", run.Status)
	require.Equal(t, "superseded", run.Error.String)
	rows, err := apprepo.New(si.conn.conn).ListLiveApplicationIDs(ctx, apprepo.ListLiveApplicationIDsParams{
		OrganizationID: si.orgID, IdentityProviderConnectionID: id,
	})
	require.NoError(t, err)
	require.Equal(t, []string{appA}, rows, "the stale apply must not replace the current snapshot")
	var got time.Time
	err = si.conn.conn.QueryRow(ctx, "SELECT applications_synced_at FROM okta_identity_provider_connections WHERE identity_provider_connection_id = $1", id).Scan(&got)
	require.NoError(t, err)
	require.True(t, watermark.Time.Equal(got), "the newer watermark must remain intact")
}
