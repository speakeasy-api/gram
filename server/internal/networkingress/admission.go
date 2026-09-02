package networkingress

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// ExpansionAdmission authorizes actions that expand private-network access.
// Durable entitlement and organization-targeted rollout clearance are both
// required. Missing, disabled, indeterminate, or errored state fails closed.
type ExpansionAdmission struct {
	features *productfeatures.Client
	flags    feature.Provider
	orgs     *orgrepo.Queries
	ready    bool
}

func NewExpansionAdmission(features *productfeatures.Client, flags feature.Provider, orgs *orgrepo.Queries, ready bool) *ExpansionAdmission {
	return &ExpansionAdmission{features: features, flags: flags, orgs: orgs, ready: ready}
}

func (a *ExpansionAdmission) CheckExpansion(ctx context.Context, organizationID string) error {
	if a == nil || a.features == nil || a.orgs == nil {
		return fmt.Errorf("network ingress admission is unavailable")
	}
	entitled, err := a.features.IsFeatureEnabledUncached(ctx, organizationID, productfeatures.FeatureNetworkIngress)
	if err != nil {
		return fmt.Errorf("check network ingress entitlement: %w", err)
	}
	if !entitled {
		return fmt.Errorf("network ingress entitlement is disabled")
	}

	org, err := a.orgs.GetOrganizationMetadata(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("organization not found")
	}
	if err != nil {
		return fmt.Errorf("load organization for network ingress rollout: %w", err)
	}
	if org.Slug == "" {
		return fmt.Errorf("organization rollout identity is unavailable")
	}

	evaluation, err := feature.EvaluateFlag(
		ctx,
		a.flags,
		feature.FlagNetworkIngressRollout,
		organizationID,
		feature.OrgProjectGroups(org.Slug, ""),
	)
	if err != nil {
		return fmt.Errorf("check network ingress rollout: %w", err)
	}
	if evaluation != feature.EvaluationEnabled {
		return fmt.Errorf("network ingress rollout is not enabled")
	}
	return nil
}

func (a *ExpansionAdmission) CheckNetworkAccess(ctx context.Context, tx pgx.Tx, input networkaccess.EligibilityInput) error {
	if input.Mode.IsPublicOnly() {
		return nil
	}
	if tx == nil {
		return fmt.Errorf("network ingress admission transaction is unavailable")
	}
	if err := a.CheckExpansion(ctx, input.OrganizationID); err != nil {
		return err
	}
	if !a.ready {
		return fmt.Errorf("network ingress reconciliation is unavailable")
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
