// Package orgcap limits how many organizations mirrored from the identity
// provider a single user can hold.
//
// Speakeasy mirrors every identity-provider organization a user belongs to,
// including ones another product created. The flows that create an
// organization here only run for a user who has none, but nothing bounds how
// many the identity provider reports. This package keeps the first and
// disables the rest.
//
// The cap is applied by the organization event sync only. Events for one
// organization arrive in order, so its members are known when the check runs.
// A login that mirrors a membership before its event is processed is not
// checked: at that moment only the signing-in user's membership is known, so a
// team could not be told apart from an organization of one.
package orgcap

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// maxOtherOrganizations is how many enabled organizations a user may already
// have before a newly mirrored one is disabled. One means a user keeps a
// single organization that no Speakeasy flow created for them. The flows that
// do create organizations here already refuse a user who has one, so this
// holds mirrored organizations to the same limit.
const maxOtherOrganizations int32 = 1

// actor is the audit principal recorded when the cap disables an organization.
var actor = urn.NewSystemPrincipal("mirrored-organization-cap")

// actorDisplayName is shown in place of the bare system principal.
const actorDisplayName = "Mirrored organization cap"

// NewMembership is a membership recorded for the first time. The cap must only
// be applied at that moment: organizations that predate creation_source also
// carry no source, and an organization staff re-enabled must stay enabled.
type NewMembership struct {
	// OrganizationID is the Speakeasy organization the user is joining.
	OrganizationID string

	// UserID is the Speakeasy user. Empty when the identity provider reports
	// a member who has not signed in to Speakeasy yet.
	UserID string

	// WorkOSUserID is the identity-provider user. It is required: the cap is
	// serialized on it, and it is the only identifier every event carries.
	WorkOSUserID string
}

// Enforce disables the organization when it was mirrored from the identity
// provider, the user is its only member, and the user already has
// maxOtherOrganizations enabled organizations. It reports whether the
// organization was disabled, and records an audit entry in tx when it was.
//
// Staff users, organizations a Speakeasy flow created, and organizations with
// SSO, directory sync or a verified domain are never disabled.
//
// The caller must already have written the membership in tx. Enforce then
// takes a per-user advisory lock held until tx ends, so concurrent
// transactions for the same user decide one after another. The decision reads
// rows committed by the transactions that went first, which requires the
// default READ COMMITTED isolation level.
func Enforce(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, membership NewMembership) (bool, error) {
	if membership.OrganizationID == "" || membership.WorkOSUserID == "" {
		return false, nil
	}

	queries := orgrepo.New(tx)
	if err := queries.LockMirroredOrganizationCap(ctx, membership.WorkOSUserID); err != nil {
		return false, fmt.Errorf("lock mirrored organization cap: %w", err)
	}

	disabled, err := queries.DisableMirroredOrganizationOverCap(ctx, orgrepo.DisableMirroredOrganizationOverCapParams{
		OrganizationID:        membership.OrganizationID,
		UserID:                conv.ToPGTextEmpty(membership.UserID),
		WorkosUserID:          conv.ToPGText(membership.WorkOSUserID),
		MaxOtherOrganizations: maxOtherOrganizations,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("disable mirrored organization over cap: %w", err)
	}

	disabledAt := disabled.DisabledAt.Time
	if err := auditLogger.LogOrganizationDisabledByMembershipCap(ctx, tx, audit.LogOrganizationAccessEvent{
		OrganizationID:             disabled.ID,
		Actor:                      actor,
		ActorDisplayName:           new(actorDisplayName),
		ActorSlug:                  nil,
		OrganizationName:           disabled.Name,
		OrganizationSlug:           disabled.Slug,
		OrganizationSnapshotBefore: &audit.OrganizationAccessSnapshot{DisabledAt: nil},
		OrganizationSnapshotAfter:  &audit.OrganizationAccessSnapshot{DisabledAt: &disabledAt},
	}); err != nil {
		return false, fmt.Errorf("audit mirrored organization cap: %w", err)
	}

	return true, nil
}
