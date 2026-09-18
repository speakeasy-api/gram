package identityproviderconnections_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	apprepo "github.com/speakeasy-api/gram/server/internal/oktaapplications/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	newer := testenv.BeginTx(t, ctx, si.conn.conn)
	_, err := apprepo.New(newer).HoldSyncConnectionFixture(ctx, id)
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
		err := si.conn.conn.QueryRow( //nolint:glint // notestingrawsql: pg_blocking_pids is a PostgreSQL test synchronization primitive unavailable to SQLc generation
			ctx, `
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
	got, err := apprepo.New(si.conn.conn).GetApplicationsSyncedAt(ctx, apprepo.GetApplicationsSyncedAtParams{IdentityProviderConnectionID: id, OrganizationID: si.orgID})
	require.NoError(t, err)
	require.True(t, watermark.Time.Equal(got.Time), "the newer watermark must remain intact")
}

// waitForLockWait blocks until a session running the named query is waiting
// on a row lock; the second waiter on a tuple reports the first as its
// blocker, so the holder is not asserted.
func waitForLockWait(t *testing.T, ctx context.Context, si *serviceInstance, queryName string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool
		err := si.conn.conn.QueryRow( //nolint:glint // notestingrawsql: pg_stat_activity is a PostgreSQL test synchronization primitive unavailable to SQLc generation
			ctx, fmt.Sprintf(`
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database()
				  AND query LIKE '-- name: %s %%'
				  AND wait_event_type = 'Lock'
			)`, queryName)).Scan(&waiting)
		require.NoError(t, err)
		return waiting
	}, 5*time.Second, 10*time.Millisecond)
}

func TestApplicationsSync_RevokeBetweenLoadAndRunCreationLeavesNoRun(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)
	setFixtures(si, []okta.App{fixtureApp(appA, "Current app", "oidc_client")}, nil, nil)
	runSync(t, ctx, syncer, id)
	require.EqualValues(t, 1, countRows(t, ctx, si, "okta_application_reconcile_runs", id))

	// Hold the connection row so revoke queues on it first and Run, which
	// has already loaded a verified target, queues behind revoke.
	holder := testenv.BeginTx(t, ctx, si.conn.conn)
	_, err := apprepo.New(holder).HoldSyncConnectionFixture(ctx, id)
	require.NoError(t, err)

	revoked := make(chan error, 1)
	var view *gen.OktaIdentityProviderConnection
	go func() {
		var err error
		view, err = si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: verified.ID})
		revoked <- err
	}()
	waitForLockWait(t, ctx, si, "LockOktaIdentityProviderConnection")

	ran := make(chan error, 1)
	go func() { ran <- syncer.Run(ctx, id, false) }()
	waitForLockWait(t, ctx, si, "LockSyncConnection")

	require.NoError(t, holder.Commit(ctx))
	for _, done := range []chan error{revoked, ran} {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}

	require.Equal(t, identityproviderconnections.StatusRevoked, view.Status)
	require.Zero(t, countRows(t, ctx, si, "okta_application_reconcile_runs", id), "a run opened after revoke would outlive the deleted history")
	require.Zero(t, countRows(t, ctx, si, "okta_applications", id))
}
