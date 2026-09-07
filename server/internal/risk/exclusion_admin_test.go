package risk

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type blockingExclusionReconciler struct {
	entered chan error
	release chan struct{}
}

func (r *blockingExclusionReconciler) Reconcile(ctx context.Context, _, _ uuid.UUID) error {
	_, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		r.entered <- context.DeadlineExceeded
	} else {
		r.entered <- ctx.Err()
	}
	<-r.release
	return nil
}

func TestExclusionAfterCommitDetachesAndReturnsImmediately(t *testing.T) {
	t.Parallel()

	reconciler := &blockingExclusionReconciler{
		entered: make(chan error, 1),
		release: make(chan struct{}),
	}
	t.Cleanup(func() {
		select {
		case <-reconciler.release:
		default:
			close(reconciler.release)
		}
	})
	logger := testenv.NewLogger(t)
	afterCommit := newExclusionAfterCommit(logger, reconciler)

	requestCtx, cancel := context.WithCancel(t.Context())
	cancel()
	returned := make(chan struct{})
	go func() {
		afterCommit(requestCtx, uuid.New(), uuid.New())
		close(returned)
	}()

	select {
	case err := <-reconciler.entered:
		require.NoError(t, err, "reconcile context must ignore request cancellation and remain bounded")
	case <-time.After(time.Second):
		require.FailNow(t, "reconciler was not invoked")
	}
	select {
	case <-returned:
	case <-time.After(time.Second):
		require.FailNow(t, "after-commit hook waited for reconciliation")
	}
	close(reconciler.release)
}
