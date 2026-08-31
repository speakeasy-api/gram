package networkaccess

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

type Mode string

const (
	ModePublicOnly  Mode = "public_only"
	ModeDual        Mode = "dual"
	ModePrivateOnly Mode = "private_only"

	// ServingPolicyVersion is incremented whenever a deployment gains a new
	// network-mode enforcement contract. Rollout tooling must verify every
	// serving pod reports at least this version before admitting non-public
	// writes, preventing mixed-version fail-open rollouts.
	ServingPolicyVersion       = 1
	ServingPolicyVersionHeader = "X-Gram-Network-Serving-Policy-Version"
)

type Surface string

const (
	SurfacePublic  Surface = "public"
	SurfacePrivate Surface = "private"
)

func Parse(value string) (Mode, error) {
	mode := Mode(value)
	switch mode {
	case ModePublicOnly, ModeDual, ModePrivateOnly:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid network access mode %q", value)
	}
}

// Effective resolves a persisted mode for policy decisions. Existing NULL and
// empty values retain expand-migration compatibility, while unknown values stay
// invalid so no serving or update path can accidentally authorize a surface.
func Effective(value pgtype.Text) (Mode, error) {
	if !value.Valid || value.String == "" {
		return ModePublicOnly, nil
	}
	mode, err := Parse(value.String)
	if err != nil {
		return "", fmt.Errorf("parse persisted network access mode: %w", err)
	}
	return mode, nil
}

// EffectiveForView keeps API responses inside the published enum. It is not a
// policy decision: unknown persisted values render as the safe recovery mode,
// while policy callers must use Effective and handle its error. If a client
// echoes this value, the corrupt row is explicitly recovered to public_only.
func EffectiveForView(value pgtype.Text) Mode {
	mode, err := Effective(value)
	if err != nil {
		return ModePublicOnly
	}
	return mode
}

// ParseRequested parses an optional API mode. An explicit value is resolved
// without consulting storage so an authorized caller can always recover a
// corrupt row to public_only; an omission strictly preserves the stored mode.
// T keeps this adapter shared without coupling the policy package to generated
// API types.
func ParseRequested[T ~string](requested *T, stored pgtype.Text) (Mode, error) {
	if requested == nil {
		return Effective(stored)
	}
	mode, err := Parse(string(*requested))
	if err != nil {
		return "", fmt.Errorf("parse requested network access mode: %w", err)
	}
	return mode, nil
}

// Storage maps public_only to NULL for expand compatibility with existing
// rows. Non-public modes are stored explicitly.
func Storage(mode Mode) pgtype.Text {
	if mode == ModePublicOnly {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: string(mode), Valid: true}
}

func (m Mode) Allows(surface Surface) bool {
	switch surface {
	case SurfacePublic:
		return m == ModePublicOnly || m == ModeDual
	case SurfacePrivate:
		return m == ModeDual || m == ModePrivateOnly
	default:
		return false
	}
}

func (m Mode) IsPublicOnly() bool {
	return m == ModePublicOnly
}

// EligibilityChecker authorizes admission to a non-public network mode. The
// control-plane implementation arrives in a later checkpoint; nil and errors
// must be treated as denial by callers.
type EligibilityChecker interface {
	CheckNetworkAccess(ctx context.Context, input EligibilityInput) error
}

type EligibilityInput struct {
	OrganizationID string
	Mode           Mode
}

type DenyAllChecker struct{}

func (DenyAllChecker) CheckNetworkAccess(context.Context, EligibilityInput) error {
	return fmt.Errorf("private network access is not enabled")
}
