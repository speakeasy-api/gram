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
		// Unknown persisted values must never make an older binary reopen the
		// public surface during a rolling deploy or after bad data is written.
		return ModePrivateOnly
	}
	return mode
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
