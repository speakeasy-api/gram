package internalsessionissuers

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
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
	// StateKeyless is an enabled issuer whose keys were all revoked after a
	// compromise and whose replacement is not active yet. It cannot sign.
	StateKeyless State = "keyless"
)

// FeatureChecker reports product entitlements, read live so a lapsed
// entitlement stops signing at once.
type FeatureChecker interface {
	IsFeatureEnabledUncached(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// Gate combines the rollout flag, the SSO entitlement, deployment readiness,
// the kill switches and the organization's issuer row into one State.
type Gate struct {
	db               repo.DBTX
	features         FeatureChecker
	flags            feature.Provider
	provisioning     bool
	paused           bool
	signingAuthority func(context.Context, string, Status) error
	enableAuthority  func(string, Status) error
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
	return &Gate{db: db, features: features, flags: flags, provisioning: cfg.Provisioning, paused: cfg.SigningPaused, signingAuthority: nil, enableAuthority: nil}
}

// SetSigningAuthority installs the deployment's readiness check. Call before
// serving status or mutations. The checker must not call Status; it receives
// the already resolved lifecycle state and key. Lifecycle-only callers may
// leave it unset, while runtime service gates always install their signer.
func (g *Gate) SetSigningAuthority(check func(context.Context, string, Status) error) {
	g.signingAuthority = check
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
	// ActiveKey is the signing key Status read; set only for an issuer that
	// reached the key check.
	ActiveKey *repo.JsonWebKey
}

// Status reads an organization's state. A flag or entitlement lookup failure
// is an error, never a silent state. A keyless issuer is checked before the
// pause switch: it cannot sign either way.
func (g *Gate) Status(ctx context.Context, organizationID string) (Status, error) {
	status := Status{State: StateUnavailable, Issuer: nil, RolledOut: false, Entitled: false, ActiveKey: nil}
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
		status.ActiveKey, err = ActiveKey(ctx, g.db, organizationID, status.Issuer.JsonWebKeySetID)
		if err != nil {
			return status, err
		}
		status.State = StateEnabled
		switch {
		case status.ActiveKey == nil:
			status.State = StateKeyless
		case g.signingPaused(ctx, organizationID, groups):
			status.State = StateSigningPaused
		}
	}
	if status.State == StateEnabled && g.signingAuthority != nil {
		if err := g.signingAuthority(ctx, organizationID, status); err != nil {
			status.State = StateUnavailable
			if !errors.Is(err, ErrMisconfigured) {
				return status, fmt.Errorf("check issuer signing authority: %w", err)
			}
		}
	}
	return status, nil
}

// ActiveKey returns the issuer set's active key, or nil when it is keyless.
func ActiveKey(ctx context.Context, db repo.DBTX, organizationID string, setID uuid.UUID) (*repo.JsonWebKey, error) {
	key, err := repo.New(db).GetActiveJsonWebKey(ctx, repo.GetActiveJsonWebKeyParams{JsonWebKeySetID: setID, OrganizationID: organizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("read active issuer key: %w", err)
	}
	return &key, nil
}

// signingPaused fails closed: signing runs only when the kill switch is
// authoritatively off. A missing flag, an unavailable provider or a lookup
// error all pause signing.
func (g *Gate) signingPaused(ctx context.Context, organizationID string, groups map[string]string) bool {
	if g.paused {
		return true
	}
	evaluation, err := feature.EvaluateFlag(ctx, g.flags, feature.FlagInternalSessionIssuerSigningPaused, organizationID, groups)
	return err != nil || evaluation != feature.EvaluationDisabled
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

// Ready reports live signing readiness for the exact issuer referenced by a
// provider order. Status includes deployment authority, rollout and entitlement.
func (g *Gate) Ready(ctx context.Context, organizationID string, issuerID uuid.UUID) (bool, error) {
	status, err := g.Status(ctx, organizationID)
	if err != nil {
		return false, err
	}
	return status.State == StateEnabled && status.Issuer != nil && status.ActiveKey != nil && status.Issuer.ID == issuerID, nil
}

// SetEnableAuthority installs the deployment check used before offering Enable.
// Disabled issuers must retain their lifecycle state for containment even when
// their saved origin belongs to another deployment.
func (g *Gate) SetEnableAuthority(check func(string, Status) error) {
	g.enableAuthority = check
}

// CanEnable separates activation eligibility from retained lifecycle state.
func (g *Gate) CanEnable(organizationID string, status Status) bool {
	if !status.RolledOut || !status.Entitled || !g.Provisioning() ||
		(status.State != StateSetupRequired && status.State != StateDisabled) {
		return false
	}
	return g.enableAuthority == nil || g.enableAuthority(organizationID, status) == nil
}
