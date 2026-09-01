package activities

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type captureConversionPolicyReconciler struct {
	keyTypes []openrouter.KeyType
	failOnce bool
	err      error
}

func (r *captureConversionPolicyReconciler) ReconcileAPIKeyConversionPolicy(_ context.Context, _ string, keyType openrouter.KeyType) error {
	r.keyTypes = append(r.keyTypes, keyType)
	if r.failOnce {
		r.failOnce = false
		return errors.New("upstream unavailable")
	}
	return r.err
}

func TestReconcileEnterpriseTrialConversionKeysProjectsCurrentPolicyForEveryKeyType(t *testing.T) {
	t.Parallel()
	reconciler := &captureConversionPolicyReconciler{}
	activity := NewReconcileEnterpriseTrialConversionKeys(testenv.NewLogger(t), reconciler)

	require.NoError(t, activity.Do(t.Context(), ReconcileEnterpriseTrialConversionKeysArgs{OrganizationID: "organization_placeholder"}))
	require.Equal(t, openrouter.AllKeyTypes, reconciler.keyTypes)
}

func TestReconcileEnterpriseTrialConversionKeysPermanentFailuresAreNonRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       ReconcileEnterpriseTrialConversionKeysArgs
		reconciler ConversionPolicyReconciler
	}{
		{name: "invalid input", args: ReconcileEnterpriseTrialConversionKeysArgs{}, reconciler: &captureConversionPolicyReconciler{}},
		{name: "missing reconciler", args: ReconcileEnterpriseTrialConversionKeysArgs{OrganizationID: "organization_placeholder"}},
		{name: "provider 400", args: ReconcileEnterpriseTrialConversionKeysArgs{OrganizationID: "organization_placeholder"}, reconciler: &captureConversionPolicyReconciler{err: &openrouter.HTTPError{StatusCode: http.StatusBadRequest}}},
		{name: "upstream identity mismatch", args: ReconcileEnterpriseTrialConversionKeysArgs{OrganizationID: "organization_placeholder"}, reconciler: &captureConversionPolicyReconciler{err: openrouter.ErrAPIKeyIdentityMismatch}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			activity := NewReconcileEnterpriseTrialConversionKeys(testenv.NewLogger(t), tt.reconciler)
			err := activity.Do(t.Context(), tt.args)
			var applicationErr *temporal.ApplicationError
			require.ErrorAs(t, err, &applicationErr)
			require.True(t, applicationErr.NonRetryable())
		})
	}
}

func TestReconcileEnterpriseTrialConversionKeysTransientFailuresRemainRetryable(t *testing.T) {
	t.Parallel()

	errs := []error{
		errors.New("transport unavailable"),
		&openrouter.HTTPError{StatusCode: http.StatusRequestTimeout},
		&openrouter.HTTPError{StatusCode: http.StatusTooManyRequests},
		&openrouter.HTTPError{StatusCode: http.StatusInternalServerError},
	}
	for _, reconcileErr := range errs {
		activity := NewReconcileEnterpriseTrialConversionKeys(testenv.NewLogger(t), &captureConversionPolicyReconciler{err: reconcileErr})
		err := activity.Do(t.Context(), ReconcileEnterpriseTrialConversionKeysArgs{OrganizationID: "organization_placeholder"})
		if applicationErr, ok := errors.AsType[*temporal.ApplicationError](err); ok {
			require.False(t, applicationErr.NonRetryable())
		}
	}
}

func TestReconcileEnterpriseTrialConversionKeysRetryReprojectsCurrentPolicy(t *testing.T) {
	t.Parallel()
	reconciler := &captureConversionPolicyReconciler{failOnce: true}
	activity := NewReconcileEnterpriseTrialConversionKeys(testenv.NewLogger(t), reconciler)
	args := ReconcileEnterpriseTrialConversionKeysArgs{OrganizationID: "organization_placeholder"}

	require.ErrorContains(t, activity.Do(t.Context(), args), "reconcile chat key conversion policy")
	require.NoError(t, activity.Do(t.Context(), args))
	require.Equal(t, []openrouter.KeyType{openrouter.KeyTypeChat, openrouter.KeyTypeChat, openrouter.KeyTypeInternal}, reconciler.keyTypes)
}
