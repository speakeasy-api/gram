package background

import (
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

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
