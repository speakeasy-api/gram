package networkingress

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// ExpansionAdmission authorizes actions that expand private-network access.
// Durable entitlement and global runtime readiness are both required. Missing,
// disabled, or errored state fails closed.
type ExpansionAdmission struct {
	features *productfeatures.Client
	ready    atomic.Bool
	enabled  bool
}

func NewExpansionAdmission(features *productfeatures.Client, ready, enabled bool) *ExpansionAdmission {
	admission := &ExpansionAdmission{features: features, ready: atomic.Bool{}, enabled: enabled}
	admission.ready.Store(ready)
	return admission
}

func (a *ExpansionAdmission) SetReconcilerReady(ready bool) {
	if a != nil {
		a.ready.Store(ready)
	}
}

func (a *ExpansionAdmission) CheckExpansion(ctx context.Context, organizationID string) error {
	if a == nil || !a.enabled {
		return fmt.Errorf("network ingress is disabled")
	}
	if a.features == nil {
		return fmt.Errorf("network ingress admission is unavailable")
	}
	entitled, err := a.features.IsFeatureEnabledUncached(ctx, organizationID, productfeatures.FeatureNetworkIngress)
	if err != nil {
		return fmt.Errorf("check network ingress entitlement: %w", err)
	}
	if !entitled {
		return fmt.Errorf("network ingress entitlement is disabled")
	}
	return nil
}

func (a *ExpansionAdmission) PrepareNetworkAccess(ctx context.Context, input networkaccess.EligibilityInput) (networkaccess.AdmissionFinalizer, error) {
	if input.Mode.IsPublicOnly() {
		return networkaccess.NewAdmissionFinalizer(func(context.Context, pgx.Tx) error { return nil }), nil
	}
	if err := a.CheckExpansion(ctx, input.OrganizationID); err != nil {
		return networkaccess.AdmissionFinalizer{}, err
	}
	if !a.ready.Load() {
		return networkaccess.AdmissionFinalizer{}, fmt.Errorf("network ingress reconciliation is unavailable")
	}
	return networkaccess.NewAdmissionFinalizer(func(ctx context.Context, tx pgx.Tx) error {
		return a.checkPreparedNetworkAccess(ctx, tx, input)
	}), nil
}

func (a *ExpansionAdmission) checkPreparedNetworkAccess(ctx context.Context, tx pgx.Tx, input networkaccess.EligibilityInput) error {
	if tx == nil {
		return fmt.Errorf("network ingress admission transaction is unavailable")
	}
	queries := networkingressrepo.New(tx)
	if err := queries.AcquireNetworkIngressOrganizationLock(ctx, input.OrganizationID); err != nil {
		return fmt.Errorf("lock network ingress admission: %w", err)
	}
	enabled, err := queries.HasEnabledNetworkIngress(ctx, input.OrganizationID)
	if err != nil {
		return fmt.Errorf("check enabled network ingress: %w", err)
	}
	if !enabled {
		return fmt.Errorf("an enabled network ingress is required")
	}
	return nil
}
