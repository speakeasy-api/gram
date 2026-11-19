package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

func TestRollbackNetworkIngressSweepRestoresBoundedCleanupContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var rollbackCtx context.Context

	rollbackNetworkIngressSweep(ctx, func(ctx context.Context) error {
		rollbackCtx = ctx
		require.NoError(t, ctx.Err())
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(5*time.Second), deadline, time.Second)
		return nil
	})

	require.NotNil(t, rollbackCtx)
}

func TestOrphanInventoryActivityErrorPreservesReconcileFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		failure   *networkingress.ReconcileError
		retryable bool
	}{
		{name: "retryable", failure: &networkingress.ReconcileError{Code: "database_unavailable", Retryable: true}, retryable: true},
		{name: "non-retryable", failure: &networkingress.ReconcileError{Code: "invalid_desired_state", Retryable: false}, retryable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := orphanInventoryActivityError(tt.failure)
			var applicationErr *temporal.ApplicationError
			require.ErrorAs(t, err, &applicationErr)
			require.Equal(t, tt.failure.Code, applicationErr.Message())
			require.Equal(t, "network_ingress", applicationErr.Type())
			require.Equal(t, tt.retryable, !applicationErr.NonRetryable())
		})
	}
}

func TestOrphanInventoryActivityErrorBoundsUnexpectedFailure(t *testing.T) {
	t.Parallel()

	err := orphanInventoryActivityError(errors.New("provider leaked a secret"))
	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationErr)
	require.Equal(t, "orphan_inventory_unavailable", applicationErr.Message())
	require.False(t, applicationErr.NonRetryable())
	require.NotContains(t, err.Error(), "provider leaked a secret")
}

func TestReconcileActivityErrorRetriesDeletionPendingQuickly(t *testing.T) {
	t.Parallel()

	err := reconcileActivityError(&networkingress.ReconcileError{Code: networkingress.ReconcileCodeDeletionPending, Retryable: true}, 1)

	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationErr)
	require.Equal(t, networkingress.ReconcileCodeDeletionPending, applicationErr.Message())
	require.Equal(t, "network_ingress", applicationErr.Type())
	require.False(t, applicationErr.NonRetryable())
	require.Equal(t, networkIngressDeletionPendingRetryDelay, applicationErr.NextRetryDelay())
}

func TestReconcileActivityErrorBacksOffDeletionPendingThatStaysStuck(t *testing.T) {
	t.Parallel()

	err := reconcileActivityError(&networkingress.ReconcileError{Code: networkingress.ReconcileCodeDeletionPending, Retryable: true}, networkIngressDeletionPendingFastAttempts)

	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationErr)
	require.False(t, applicationErr.NonRetryable())
	require.Zero(t, applicationErr.NextRetryDelay())
}

func TestReconcileActivityErrorKeepsBackoffForOtherFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		code      string
		retryable bool
	}{
		{name: "retryable", err: &networkingress.ReconcileError{Code: "provider_error", Retryable: true}, code: "provider_error", retryable: true},
		{name: "non-retryable", err: &networkingress.ReconcileError{Code: "invalid_desired_state", Retryable: false}, code: "invalid_desired_state", retryable: false},
		{name: "unclassified", err: errors.New("provider leaked a secret"), code: "reconciliation_failed", retryable: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := reconcileActivityError(tt.err, 1)
			var applicationErr *temporal.ApplicationError
			require.ErrorAs(t, err, &applicationErr)
			require.Equal(t, tt.code, applicationErr.Message())
			require.Equal(t, tt.retryable, !applicationErr.NonRetryable())
			require.Zero(t, applicationErr.NextRetryDelay())
			require.NotContains(t, err.Error(), "secret")
		})
	}
}
