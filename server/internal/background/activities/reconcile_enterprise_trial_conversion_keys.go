package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"go.temporal.io/sdk/temporal"
)

type ConversionPolicyReconciler interface {
	ReconcileAPIKeyConversionPolicy(context.Context, string, openrouter.KeyType) error
}

type ReconcileEnterpriseTrialConversionKeys struct {
	reconciler ConversionPolicyReconciler
}

const enterpriseTrialConversionPermanentErrorType = "enterprise-trial-conversion-permanent"

type ReconcileEnterpriseTrialConversionKeysArgs struct {
	OrganizationID string
}

func NewReconcileEnterpriseTrialConversionKeys(_ *slog.Logger, reconciler ConversionPolicyReconciler) *ReconcileEnterpriseTrialConversionKeys {
	return &ReconcileEnterpriseTrialConversionKeys{reconciler: reconciler}
}

func (r *ReconcileEnterpriseTrialConversionKeys) Do(ctx context.Context, args ReconcileEnterpriseTrialConversionKeysArgs) error {
	if args.OrganizationID == "" {
		return temporal.NewNonRetryableApplicationError("organization ID is required", enterpriseTrialConversionPermanentErrorType, nil)
	}
	if r.reconciler == nil {
		return temporal.NewNonRetryableApplicationError("OpenRouter conversion policy reconciler is unavailable", enterpriseTrialConversionPermanentErrorType, nil)
	}
	for _, keyType := range openrouter.AllKeyTypes {
		if err := r.reconciler.ReconcileAPIKeyConversionPolicy(ctx, args.OrganizationID, keyType); err != nil {
			wrapped := fmt.Errorf("reconcile %s key conversion policy: %w", keyType, err)
			if openrouter.IsPermanentError(err) {
				return temporal.NewNonRetryableApplicationError(wrapped.Error(), enterpriseTrialConversionPermanentErrorType, wrapped)
			}
			return wrapped
		}
	}
	return nil
}
