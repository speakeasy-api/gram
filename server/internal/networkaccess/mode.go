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

func Effective(value pgtype.Text) Mode {
	if !value.Valid || value.String == "" {
		return ModePublicOnly
	}
	mode, err := Parse(value.String)
	if err != nil {
		// Views must stay inside the published API enum. Serving paths use
		// EffectiveValidated and deny unknown values rather than relying on this
		// restrictive representation.
		return ModePrivateOnly
	}
	return mode
}

// ParseRequested parses an optional API mode and preserves fallback when the
// field is omitted. T keeps this adapter shared without coupling the policy
// package to generated API types.
func ParseRequested[T ~string](requested *T, fallback Mode) (Mode, error) {
	if requested == nil {
		return fallback, nil
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
