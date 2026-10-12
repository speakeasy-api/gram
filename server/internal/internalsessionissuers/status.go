package internalsessionissuers

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// State is what an organization can do with its issuer. The management API,
// the Platform MCP tool and the signer all read it from Gate.Status.
type State string

const (
	StateUnavailable         State = "unavailable"
	StateEntitlementRequired State = "entitlement_required"
	StateSetupRequired       State = "setup_required"
	StateEnabled             State = "enabled"
	StateDisabled            State = "disabled"
	StateSigningPaused       State = "signing_paused"
)

// FeatureChecker reports product entitlements, read live so a lapsed
// entitlement stops signing at once.
type FeatureChecker interface {
	IsFeatureEnabledUncached(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// Gate combines the rollout flag, the SSO entitlement, deployment readiness,
// the kill switches and the organization's issuer row into one State.
type Gate struct {
	db           repo.DBTX
	features     FeatureChecker
	flags        feature.Provider
	provisioning bool
	paused       bool
}

// GateConfig is deployment configuration for a Gate.
type GateConfig struct {
	// Provisioning is whether this deployment can create issuer keys.
	Provisioning bool
	// SigningPaused is the startup kill switch; the
	// FlagInternalSessionIssuerSigningPaused flag pauses without a restart.
	SigningPaused bool
}

func NewGate(db repo.DBTX, features FeatureChecker, flags feature.Provider, cfg GateConfig) *Gate {
	return &Gate{db: db, features: features, flags: flags, provisioning: cfg.Provisioning, paused: cfg.SigningPaused}
}

// Provisioning reports whether this deployment can create issuer keys.
func (g *Gate) Provisioning() bool { return g != nil && g.provisioning }

// Status is an organization's issuer state with its row, when one exists.
type Status struct {
	State State
	// Issuer is nil when the organization never enabled one.
	Issuer *repo.InternalSessionIssuer
	// RolledOut is the rollout flag; Entitled is the SSO entitlement.
	RolledOut bool
	Entitled  bool
}

// Status reads an organization's state. A flag or entitlement lookup failure
// is an error, never a silent state.
func (g *Gate) Status(ctx context.Context, organizationID string) (Status, error) {
	status := Status{State: StateUnavailable, Issuer: nil, RolledOut: false, Entitled: false}
	row, err := repo.New(g.db).GetOrganizationInternalSessionIssuer(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return status, fmt.Errorf("read issuer: %w", err)
	default:
		status.Issuer = &row
	}
	groups, err := g.groups(ctx, organizationID)
	if err != nil {
		return status, err
	}
	status.RolledOut, err = g.flagEnabled(ctx, feature.FlagInternalSessionIssuer, organizationID, groups)
	if err != nil {
		return status, fmt.Errorf("read issuer rollout: %w", err)
	}
	status.Entitled, err = g.features.IsFeatureEnabledUncached(ctx, organizationID, productfeatures.FeatureSSO)
	if err != nil {
		return status, fmt.Errorf("read sso entitlement: %w", err)
	}
	switch {
	case status.Issuer != nil && status.Issuer.DisabledAt.Valid:
		status.State = StateDisabled
	case !status.RolledOut:
		status.State = StateUnavailable
	case !status.Entitled:
		status.State = StateEntitlementRequired
	case status.Issuer == nil && !g.provisioning:
		status.State = StateUnavailable
	case status.Issuer == nil:
		status.State = StateSetupRequired
	default:
		paused, err := g.signingPaused(ctx, organizationID, groups)
		if err != nil {
			return status, err
		}
		status.State = StateEnabled
		if paused {
			status.State = StateSigningPaused
		}
	}
	return status, nil
}

// signingPaused fails closed: a flag lookup error pauses signing.
func (g *Gate) signingPaused(ctx context.Context, organizationID string, groups map[string]string) (bool, error) {
	if g.paused {
		return true, nil
	}
	paused, err := g.flagEnabled(ctx, feature.FlagInternalSessionIssuerSigningPaused, organizationID, groups)
	if err != nil {
		return true, nil //nolint:nilerr // A kill switch that can't be read pauses signing.
	}
	return paused, nil
}

func (g *Gate) flagEnabled(ctx context.Context, flag feature.Flag, organizationID string, groups map[string]string) (bool, error) {
	if g.flags == nil {
		return false, nil
	}
	enabled, err := g.flags.IsFlagEnabled(ctx, flag, organizationID, groups)
	if err != nil {
		return false, fmt.Errorf("evaluate %s: %w", flag, err)
	}
	return enabled, nil
}

func (g *Gate) groups(ctx context.Context, organizationID string) (map[string]string, error) {
	org, err := orgrepo.New(g.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("read organization: %w", err)
	}
	return feature.OrgProjectGroups(org.Slug, ""), nil
}
