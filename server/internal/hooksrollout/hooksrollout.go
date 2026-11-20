// Package hooksrollout stores and resolves the hooks version rollout pins that
// Speakeasy staff set from the admin dashboard.
//
// A pin names the highest hooks generator version an organization is cleared
// to receive. The plugin publisher regenerates an organization's observability
// (hooks) plugin only once the organization's effective pin reaches the current
// generator version, so a generator bump never reaches customers on its own:
// promoting a version is the deliberate act of raising a pin.
//
// An organization's effective pin is its own override when it has one, and the
// platform-wide default pin otherwise. Canary organizations bypass pins and
// always receive the current version.
package hooksrollout

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooksrollout/repo"
)

// RecentChangesLimit bounds the change history the admin dashboard shows. Pins
// change a handful of times per hooks release, so this covers several releases.
const RecentChangesLimit = 25

// canaryOrganizationSlugs always receive the current hooks generator version,
// bypassing pins. This lives in code rather than in the pin table so that our
// own team can never be stranded on a stale hooks version by a missing or
// mistaken pin.
var canaryOrganizationSlugs = map[string]bool{
	"speakeasy-team": true,
}

// IsCanary reports whether the organization always receives the current hooks
// generator version.
func IsCanary(organizationSlug string) bool {
	return canaryOrganizationSlugs[organizationSlug]
}

// CanaryOrganizationSlugs lists the canary organizations in a stable order.
func CanaryOrganizationSlugs() []string {
	return slices.Sorted(maps.Keys(canaryOrganizationSlugs))
}

// Source names where an organization's effective pin comes from.
type Source string

const (
	// SourceCanary means the organization is a canary and ignores pins.
	SourceCanary Source = "canary"

	// SourceOrganization means the organization's own override applies.
	SourceOrganization Source = "organization"

	// SourceDefault means the platform-wide default pin applies.
	SourceDefault Source = "default"

	// SourceUnset means no pin applies: there is no override and no default
	// pin has been set yet.
	SourceUnset Source = "unset"
)

// Pin is one hooks rollout pin.
type Pin struct {
	// Version is the highest hooks generator version the pin clears.
	Version int

	// SetBy is the email of the staff operator who set the pin.
	SetBy string

	// SetAt is when the pin was set.
	SetAt time.Time
}

// OrganizationPins holds the pins that can apply to one organization.
type OrganizationPins struct {
	// Override is the organization's own pin, nil when it has none.
	Override *Pin

	// Default is the platform-wide default pin, nil until one is set.
	Default *Pin
}

// Effective returns the pin that applies to the organization and where it
// comes from. It returns a nil pin with SourceUnset when neither applies.
// Canary status is the caller's to check, since it depends on the slug.
func (p OrganizationPins) Effective() (*Pin, Source) {
	switch {
	case p.Override != nil:
		return p.Override, SourceOrganization
	case p.Default != nil:
		return p.Default, SourceDefault
	default:
		return nil, SourceUnset
	}
}

// Override is an organization's current override, with the organization's
// name and slug for display.
type Override struct {
	// OrganizationID is the organization the override applies to.
	OrganizationID string

	// OrganizationName is the organization's display name.
	OrganizationName string

	// OrganizationSlug is the organization's slug.
	OrganizationSlug string

	// Pin is the override itself.
	Pin Pin
}

// Change is one entry in the pin history.
type Change struct {
	// OrganizationID is the organization whose override changed, empty for a
	// change to the default pin.
	OrganizationID string

	// OrganizationSlug is the slug of OrganizationID, empty for a change to the
	// default pin.
	OrganizationSlug string

	// Version is the version the change set, nil when it cleared an override.
	Version *int

	// SetBy is the email of the staff operator who made the change.
	SetBy string

	// SetAt is when the change was made.
	SetAt time.Time
}

// PinReader reads the pins that apply to an organization.
type PinReader interface {
	OrganizationPins(ctx context.Context, organizationID string) (OrganizationPins, error)
}

// Store reads and writes pins. Writes append to the pin history; the newest
// row in a scope is the current pin.
type Store struct {
	queries *repo.Queries
}

var _ PinReader = (*Store)(nil)

func NewStore(db repo.DBTX) *Store {
	return &Store{queries: repo.New(db)}
}

// OrganizationPins reads the organization's override and the default pin.
func (s *Store) OrganizationPins(ctx context.Context, organizationID string) (OrganizationPins, error) {
	override, err := s.organizationOverride(ctx, organizationID)
	if err != nil {
		return OrganizationPins{}, err
	}
	defaultPin, err := s.DefaultPin(ctx)
	if err != nil {
		return OrganizationPins{}, err
	}
	return OrganizationPins{Override: override, Default: defaultPin}, nil
}

// DefaultPin reads the default pin, nil when none has been set.
func (s *Store) DefaultPin(ctx context.Context) (*Pin, error) {
	row, err := s.queries.GetDefaultHooksRolloutPin(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("get default hooks rollout pin: %w", err)
	}
	return pinFromRow(row), nil
}

func (s *Store) organizationOverride(ctx context.Context, organizationID string) (*Pin, error) {
	row, err := s.queries.GetOrganizationHooksRolloutPin(ctx, conv.ToPGText(organizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("get organization hooks rollout pin: %w", err)
	}
	return pinFromRow(row), nil
}

// SetDefault moves the default pin to version. Setting the version it already
// has records nothing.
func (s *Store) SetDefault(ctx context.Context, version int, setBy string) error {
	current, err := s.DefaultPin(ctx)
	if err != nil {
		return err
	}
	if current != nil && current.Version == version {
		return nil
	}
	if _, err := s.queries.InsertHooksRolloutPin(ctx, repo.InsertHooksRolloutPinParams{
		OrganizationID: pgtype.Text{String: "", Valid: false},
		Version:        pgtype.Int4{Int32: conv.SafeInt32(version), Valid: true},
		SetBy:          setBy,
	}); err != nil {
		return fmt.Errorf("insert default hooks rollout pin: %w", err)
	}
	return nil
}

// SetOrganizationOverride pins the organization to version regardless of the
// default pin. Setting the version it already has records nothing.
func (s *Store) SetOrganizationOverride(ctx context.Context, organizationID string, version int, setBy string) error {
	current, err := s.organizationOverride(ctx, organizationID)
	if err != nil {
		return err
	}
	if current != nil && current.Version == version {
		return nil
	}
	if _, err := s.queries.InsertHooksRolloutPin(ctx, repo.InsertHooksRolloutPinParams{
		OrganizationID: conv.ToPGText(organizationID),
		Version:        pgtype.Int4{Int32: conv.SafeInt32(version), Valid: true},
		SetBy:          setBy,
	}); err != nil {
		return fmt.Errorf("insert organization hooks rollout pin: %w", err)
	}
	return nil
}

// ClearOrganizationOverride returns the organization to the default pin.
// Clearing an organization with no override records nothing.
func (s *Store) ClearOrganizationOverride(ctx context.Context, organizationID string, setBy string) error {
	current, err := s.organizationOverride(ctx, organizationID)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	if _, err := s.queries.InsertHooksRolloutPin(ctx, repo.InsertHooksRolloutPinParams{
		OrganizationID: conv.ToPGText(organizationID),
		Version:        pgtype.Int4{Int32: 0, Valid: false},
		SetBy:          setBy,
	}); err != nil {
		return fmt.Errorf("clear organization hooks rollout pin: %w", err)
	}
	return nil
}

// ListOverrides lists every organization with an override, by slug.
func (s *Store) ListOverrides(ctx context.Context) ([]Override, error) {
	rows, err := s.queries.ListOrganizationHooksRolloutOverrides(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organization hooks rollout overrides: %w", err)
	}
	overrides := make([]Override, 0, len(rows))
	for _, row := range rows {
		overrides = append(overrides, Override{
			OrganizationID:   row.OrganizationID,
			OrganizationName: row.OrganizationName,
			OrganizationSlug: row.OrganizationSlug,
			Pin:              Pin{Version: int(row.Version), SetBy: row.SetBy, SetAt: row.CreatedAt.Time},
		})
	}
	return overrides, nil
}

// ListRecentChanges lists the newest pin changes first, at most limit of them.
func (s *Store) ListRecentChanges(ctx context.Context, limit int) ([]Change, error) {
	rows, err := s.queries.ListRecentHooksRolloutChanges(ctx, conv.SafeInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("list recent hooks rollout changes: %w", err)
	}
	changes := make([]Change, 0, len(rows))
	for _, row := range rows {
		var version *int
		if row.Version.Valid {
			version = new(int(row.Version.Int32))
		}
		changes = append(changes, Change{
			OrganizationID:   conv.FromPGTextOrEmpty[string](row.OrganizationID),
			OrganizationSlug: conv.FromPGTextOrEmpty[string](row.OrganizationSlug),
			Version:          version,
			SetBy:            row.SetBy,
			SetAt:            row.CreatedAt.Time,
		})
	}
	return changes, nil
}

// pinFromRow returns nil for a row that cleared an override.
func pinFromRow(row repo.HooksRolloutPin) *Pin {
	if !row.Version.Valid {
		return nil
	}
	return &Pin{Version: int(row.Version.Int32), SetBy: row.SetBy, SetAt: row.CreatedAt.Time}
}
