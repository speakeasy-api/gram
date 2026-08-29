package activities

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type captureConversionPolicyReconciler struct {
	keyTypes []openrouter.KeyType
	failOnce bool
}

func (r *captureConversionPolicyReconciler) ReconcileAPIKeyConversionPolicy(_ context.Context, _ string, keyType openrouter.KeyType) error {
	r.keyTypes = append(r.keyTypes, keyType)
	if r.failOnce {
		r.failOnce = false
		return errors.New("upstream unavailable")
	}
	return nil
}

func TestReconcileEnterpriseTrialConversionKeysProjectsCurrentPolicyForEveryKeyType(t *testing.T) {
	t.Parallel()
	reconciler := &captureConversionPolicyReconciler{}
	activity := NewReconcileEnterpriseTrialConversionKeys(testenv.NewLogger(t), reconciler)

	require.NoError(t, activity.Do(t.Context(), ReconcileEnterpriseTrialConversionKeysArgs{OrganizationID: "organization_placeholder"}))
	require.Equal(t, openrouter.AllKeyTypes, reconciler.keyTypes)
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
