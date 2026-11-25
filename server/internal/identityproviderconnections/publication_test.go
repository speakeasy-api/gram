package identityproviderconnections

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// Losing COMMIT's response is not evidence that the key is unreferenced.
type lostCommitResponse struct {
	pgx.Tx
	referenced bool
}

func (tx *lostCommitResponse) Commit(context.Context) error {
	tx.referenced = true
	return io.ErrUnexpectedEOF
}

// A failure after COMMIT succeeded is an uncertain publication: the key is
// referenced, so cleanup must reconcile rather than disable it.
func TestUnobservedPublicationIsUncertain(t *testing.T) {
	t.Parallel()
	err := unobservedPublicationError("test-org", io.ErrUnexpectedEOF)
	var uncertain *publicationCommitError
	require.ErrorAs(t, err, &uncertain)
	require.Equal(t, "test-org", uncertain.organizationID)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestPublicationCommitReconciliation(t *testing.T) {
	t.Parallel()
	tx := &lostCommitResponse{}
	err := commitPublication(t.Context(), tx, "test-org")
	var uncertain *publicationCommitError
	require.ErrorAs(t, err, &uncertain)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.True(t, tx.referenced)

	for _, tc := range []struct {
		name       string
		absent     bool
		lookupErr  error
		wantAbsent bool
	}{
		{name: "commit succeeded but response lost", absent: !tx.referenced},
		{name: "rollback confirmed", absent: true, wantAbsent: true},
		{name: "database unavailable", lookupErr: errors.New("unavailable")},
		{name: "absence with failed lookup is inconclusive", absent: true, lookupErr: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			absent, err := reconcilePublication(ctx, func(checkCtx context.Context) (bool, error) {
				require.NoError(t, checkCtx.Err(), "canceled request must not cancel reconciliation")
				deadline, ok := checkCtx.Deadline()
				require.True(t, ok)
				require.WithinDuration(t, time.Now().Add(keyCleanupTimeout), deadline, time.Second)
				return tc.absent, tc.lookupErr
			})
			require.Equal(t, tc.wantAbsent, absent, "disable only after confirmed absence")
			if tc.lookupErr != nil {
				require.ErrorIs(t, err, tc.lookupErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
