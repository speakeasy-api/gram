package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type ConversionPolicyReconciler interface {
	ReconcileAPIKeyConversionPolicy(context.Context, string, openrouter.KeyType) error
}

type ReconcileEnterpriseTrialConversionKeys struct {
	reconciler ConversionPolicyReconciler
}

type ReconcileEnterpriseTrialConversionKeysArgs struct {
	OrganizationID string
}

func NewReconcileEnterpriseTrialConversionKeys(_ *slog.Logger, reconciler ConversionPolicyReconciler) *ReconcileEnterpriseTrialConversionKeys {
	return &ReconcileEnterpriseTrialConversionKeys{reconciler: reconciler}
}

func (r *ReconcileEnterpriseTrialConversionKeys) Do(ctx context.Context, args ReconcileEnterpriseTrialConversionKeysArgs) error {
	if args.OrganizationID == "" {
		return errors.New("organization ID is required")
	}
	if r.reconciler == nil {
		return errors.New("OpenRouter conversion policy reconciler is unavailable")
	}
	for _, keyType := range openrouter.AllKeyTypes {
		if err := r.reconciler.ReconcileAPIKeyConversionPolicy(ctx, args.OrganizationID, keyType); err != nil {
			return fmt.Errorf("reconcile %s key conversion policy: %w", keyType, err)
		}
	}
	return nil
}
