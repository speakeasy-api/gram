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
	active   ActiveIngressChecker
}

// ActiveIngressChecker lets network-mode writes additionally require a live,
// enabled desired-state row without coupling networkaccess to this package.
type ActiveIngressChecker interface {
	HasEnabledIngress(ctx context.Context, organizationID string) (bool, error)
}

type RepositoryActiveIngressChecker struct{ queries *networkingressrepo.Queries }

func NewRepositoryActiveIngressChecker(db networkingressrepo.DBTX) *RepositoryActiveIngressChecker {
	return &RepositoryActiveIngressChecker{queries: networkingressrepo.New(db)}
}

func (c *RepositoryActiveIngressChecker) HasEnabledIngress(ctx context.Context, organizationID string) (bool, error) {
	if c == nil || c.queries == nil {
		return false, fmt.Errorf("network ingress desired-state checker is unavailable")
	}
	enabled, err := c.queries.HasEnabledNetworkIngress(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("query enabled network ingress: %w", err)
	}
	return enabled, nil
}

func NewExpansionAdmission(features *productfeatures.Client, flags feature.Provider, orgs *orgrepo.Queries, active ActiveIngressChecker) *ExpansionAdmission {
	return &ExpansionAdmission{features: features, flags: flags, orgs: orgs, active: active}
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

func (a *ExpansionAdmission) CheckNetworkAccess(ctx context.Context, input networkaccess.EligibilityInput) error {
	if input.Mode.IsPublicOnly() {
		return nil
	}
	if err := a.CheckExpansion(ctx, input.OrganizationID); err != nil {
		return err
	}
	if a.active == nil {
		return fmt.Errorf("network ingress desired-state checker is unavailable")
	}
	enabled, err := a.active.HasEnabledIngress(ctx, input.OrganizationID)
	if err != nil {
		return fmt.Errorf("check enabled network ingress: %w", err)
	}
	if !enabled {
		return fmt.Errorf("an enabled network ingress is required")
	}
	return nil
}
