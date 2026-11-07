package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func refreshBoundsTx(t *testing.T, ctx context.Context, db *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: exercise bounded refresh storage with real committed fixture transactions.
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

func TestCatalogRefreshBoundsConcurrentPlanAdmission(t *testing.T) {
	t.Parallel()
	ctx, _, db := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	expires := time.Now().Add(time.Hour)
	seed := refreshBoundsTx(t, ctx, db)
	for range 31 {
		require.NoError(t, refreshSavePlan(ctx, seed, uuid.New(), "source", "owner", expires, json.RawMessage(`{}`)))
	}
	require.NoError(t, seed.Commit(ctx))
	// The final slot is uncommitted when a second backend attempts admission.
	winner := refreshBoundsTx(t, ctx, db)
	winnerID, loserID := uuid.New(), uuid.New()
	require.NoError(t, refreshSavePlan(ctx, winner, winnerID, "source", "owner", expires, json.RawMessage(`{}`)))
	loser := refreshBoundsTx(t, ctx, db)
	pid := loser.Conn().PgConn().PID()
	done := make(chan error, 1)
	go func() {
		err := refreshSavePlan(ctx, loser, loserID, "other-source", "other-owner", expires, json.RawMessage(`{}`))
		if err == nil {
			err = loser.Commit(ctx)
		} else {
			_ = loser.Rollback(ctx)
		}
		done <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND locktype = 'advisory' AND NOT granted)`, pid).Scan(&waiting) //nolint:glint // notestingrawsql: observe concurrent admission blocked behind the final slot transaction.
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, winner.Commit(ctx))
	select {
	case err := <-done:
		require.EqualError(t, err, "catalog refresh active plan limit reached")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	verify := refreshBoundsTx(t, ctx, db)
	_, _, _, err := refreshLoadPlan(ctx, verify, winnerID, "source", "owner")
	require.NoError(t, err)
	_, _, _, err = refreshLoadPlan(ctx, verify, loserID, "other-source", "other-owner")
	require.ErrorIs(t, err, ErrNotFound)
	var count int
	require.NoError(t, verify.QueryRow(ctx, `SELECT count(*) FROM mcp_catalog_refresh_plans WHERE receipt IS NULL`).Scan(&count)) //nolint:glint // notestingrawsql: assert the global admission invariant independently of helper results.
	require.Equal(t, 32, count)
	require.NoError(t, verify.Rollback(ctx))
}

func TestCatalogRefreshBoundsReceiptRetentionAndExpiry(t *testing.T) {
	t.Parallel()
	ctx, _, db := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ids := make([]uuid.UUID, 129)
	for i := range ids {
		ids[i] = uuid.New()
		tx := refreshBoundsTx(t, ctx, db)
		// Completed receipts remain replayable even after their original plan expiry.
		require.NoError(t, refreshSavePlan(ctx, tx, ids[i], "source", "owner", time.Now().Add(-time.Hour), json.RawMessage(`{"preview":true}`)))
		require.NoError(t, refreshSaveReceipt(ctx, tx, ids[i], json.RawMessage(fmt.Sprintf(`{"sequence":%d}`, i))))
		require.NoError(t, tx.Commit(ctx))
	}
	expiredID, pendingID := uuid.New(), uuid.New()
	tx := refreshBoundsTx(t, ctx, db)
	require.NoError(t, refreshSavePlan(ctx, tx, expiredID, "source", "owner", time.Now().Add(-time.Hour), json.RawMessage(`{}`)))
	require.NoError(t, tx.Commit(ctx))
	tx = refreshBoundsTx(t, ctx, db)
	require.NoError(t, refreshSavePlan(ctx, tx, pendingID, "source", "owner", time.Now().Add(time.Hour), json.RawMessage(`{}`)))
	require.NoError(t, tx.Commit(ctx))
	verify := refreshBoundsTx(t, ctx, db)
	_, _, _, err := refreshLoadPlan(ctx, verify, expiredID, "source", "owner")
	require.ErrorIs(t, err, ErrNotFound)
	_, _, _, err = refreshLoadPlan(ctx, verify, ids[0], "source", "owner")
	require.ErrorIs(t, err, ErrNotFound, "oldest completed receipt must be evicted")
	for i, id := range ids[1:] {
		_, receipt, _, err := refreshLoadPlan(ctx, verify, id, "source", "owner")
		require.NoError(t, err)
		require.JSONEq(t, fmt.Sprintf(`{"sequence":%d}`, i+1), string(receipt))
	}
	_, receipt, _, err := refreshLoadPlan(ctx, verify, pendingID, "source", "owner")
	require.NoError(t, err)
	require.Empty(t, receipt)
	var completed, pending int
	require.NoError(t, verify.QueryRow(ctx, `SELECT count(*) FILTER (WHERE receipt IS NOT NULL), count(*) FILTER (WHERE receipt IS NULL) FROM mcp_catalog_refresh_plans`).Scan(&completed, &pending)) //nolint:glint // notestingrawsql: independently verify committed retention and expired-plan pruning bounds.
	require.Equal(t, 128, completed)
	require.Equal(t, 1, pending)
	require.NoError(t, verify.Rollback(ctx))
}

func TestCatalogRefreshBoundsBaselineExhaustionRollsBackApply(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	state := refreshState{Baseline: make(map[string]json.RawMessage, 10000), Absent: make(map[string]bool, 10000)}
	for i := range 10000 {
		name := fmt.Sprintf("example.test/removed-%05d", i)
		state.Baseline[name] = json.RawMessage(strings.Replace(basicRecord, "example.test/demo", name, 1))
		state.Absent[name] = true
	}
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	tx := refreshBoundsTx(t, ctx, db)
	require.NoError(t, refreshSaveState(ctx, tx, "source", raw))
	require.NoError(t, tx.Commit(ctx))
	r := NewCatalogRefresher(s, "local", "source", refreshSourceFunc(func(context.Context) (CatalogBundle, error) {
		return refreshTestBundle(basicRecord), nil
	}), nil)
	plan, err := r.Preview(ctx, "owner")
	require.NoError(t, err)
	require.Contains(t, plan.Changes, CatalogRefreshChange{Name: "example.test/demo", Action: "create"})
	_, err = r.Apply(ctx, "owner", plan.ID)
	require.EqualError(t, err, "catalog refresh baseline exceeds entry limit")
	_, err = s.GetByName(ctx, "example.test/demo")
	require.ErrorIs(t, err, ErrNotFound, "the create preceding baseline admission must roll back")
	verify := refreshBoundsTx(t, ctx, db)
	_, receipt, _, err := refreshLoadPlan(ctx, verify, plan.ID, "source", "owner")
	require.NoError(t, err)
	require.Empty(t, receipt, "failed apply must not persist a success receipt")
	stored, err := refreshLoadState(ctx, verify, "source")
	require.NoError(t, err)
	var after refreshState
	require.NoError(t, json.Unmarshal(stored, &after))
	require.Len(t, after.Baseline, len(state.Baseline))
	for name, expected := range state.Baseline {
		require.JSONEq(t, string(expected), string(after.Baseline[name]), name)
	}
	require.Equal(t, state.Absent, after.Absent)
	require.Nil(t, after.LastFullSuccessAt)
	require.Empty(t, after.LastFullSuccessRevision)
	require.NotEqual(t, "completed", after.LastAttemptState)
	require.NoError(t, verify.Rollback(ctx))
}
