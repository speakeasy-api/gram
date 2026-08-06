package pgwal

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// lsnPattern matches Postgres pg_lsn text form: two hex segments joined by "/".
var lsnPattern = regexp.MustCompile(`^[0-9A-Fa-f]+/[0-9A-Fa-f]+$`)

func TestCurrentLSN_ReturnsWellFormedLSN(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	ti := newPGWALTestInstance(t)

	lsn, err := CurrentLSN(ctx, ti.conn)
	require.NoError(t, err)
	require.Regexp(t, lsnPattern, lsn.String())
}

func TestWaitForLSN_PrimaryCaughtUp(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	ti := newPGWALTestInstance(t)

	lsn, err := CurrentLSN(ctx, ti.conn)
	require.NoError(t, err)

	// On a primary pg_last_wal_replay_lsn() is NULL, so any target is
	// considered caught up and WaitForLSN returns promptly.
	err = WaitForLSN(ctx, ti.conn, lsn, WithTimeout(2*time.Second))
	require.NoError(t, err)
}

func TestWaitForLSN_StubImmediatelyCaughtUp(t *testing.T) {
	t.Parallel()

	db := &stubDB{caughtUp: []bool{true}}

	err := WaitForLSN(t.Context(), db, LSN{value: "0/0"}, WithPollInterval(time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, 1, db.calls, "should stop after the first successful check")
}

func TestWaitForLSN_StubPollsUntilCaughtUp(t *testing.T) {
	t.Parallel()

	db := &stubDB{caughtUp: []bool{false, false, true}}

	err := WaitForLSN(t.Context(), db, LSN{value: "0/0"}, WithPollInterval(time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, 3, db.calls)
}

func TestWaitForLSN_TimeoutReturnsSentinel(t *testing.T) {
	t.Parallel()

	db := &stubDB{caughtUp: []bool{false}}

	start := time.Now()
	err := WaitForLSN(t.Context(), db, LSN{value: "FF/FF"},
		WithPollInterval(5*time.Millisecond),
		WithTimeout(50*time.Millisecond),
	)
	require.ErrorIs(t, err, ErrWaitTimeout)
	require.NotErrorIs(t, err, context.DeadlineExceeded)
	require.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)
}

func TestWaitForLSN_ParentContextCancellation(t *testing.T) {
	t.Parallel()

	db := &stubDB{caughtUp: []bool{false}}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	// No WithTimeout option: cancellation must surface as the context error,
	// not the pgwal sentinel.
	err := WaitForLSN(ctx, db, LSN{value: "FF/FF"}, WithPollInterval(5*time.Millisecond))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, ErrWaitTimeout)
}

// stubDB is an in-memory DBTX that returns a scripted sequence of caught_up
// values, clamping to the final value once exhausted. It lets the poll-loop
// tests run without a real replica (which the single primary test container
// cannot provide, since pg_last_wal_replay_lsn() is always NULL there).
type stubDB struct {
	caughtUp []bool
	calls    int
}

func (s *stubDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	idx := s.calls
	if idx >= len(s.caughtUp) {
		idx = len(s.caughtUp) - 1
	}
	s.calls++

	return stubRow{val: s.caughtUp[idx]}
}

func (s *stubDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("stubDB.Exec should not be called")
}

func (s *stubDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("stubDB.Query should not be called")
}

type stubRow struct {
	val bool
}

func (r stubRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		panic("stubRow.Scan expects exactly one destination")
	}
	p, ok := dest[0].(*bool)
	if !ok {
		panic("stubRow.Scan expects a *bool destination")
	}
	*p = r.val

	return nil
}
