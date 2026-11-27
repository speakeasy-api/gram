// Package orgcap is the per-user organization limit. Self-serve creation checks
// it before creating, and the identity-provider membership sync disables an
// organization over it. Invitations and the login-time mirror do not apply it.
package orgcap

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// MaxOrganizationsPerUser is how many enabled organizations a user may hold
// without staff involvement.
const MaxOrganizationsPerUser int32 = 1

var ErrUserHasOrganization = errors.New("user already belongs to an organization")

var actor = urn.NewSystemPrincipal("organization-cap")

const actorDisplayName = "Organization cap"

// RequireRoomForOrganization refuses a user who already holds
// MaxOrganizationsPerUser enabled organizations other than excludeOrganizationID.
func RequireRoomForOrganization(ctx context.Context, db orgrepo.DBTX, userID, excludeOrganizationID string) error {
	existing, err := orgrepo.New(db).ListOrganizationsForUser(ctx, conv.ToPGText(userID))
	if err != nil {
		return fmt.Errorf("list user organizations: %w", err)
	}
	others := 0
	for _, organization := range existing {
		if organization.ID != excludeOrganizationID {
			others++
		}
	}
	if others >= int(MaxOrganizationsPerUser) {
		return ErrUserHasOrganization
	}
	return nil
}

type NewMembership struct {
	OrganizationID string
	UserID         string
	WorkOSUserID   string
}

// Enforce disables an organization the user joins from the identity provider
// when it would take them past MaxOrganizationsPerUser. Call it once, in the
// READ COMMITTED transaction that has just written the membership.
func Enforce(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, membership NewMembership) (bool, error) {
	if membership.OrganizationID == "" || membership.WorkOSUserID == "" {
		return false, nil
	}
	ctx = contextvalues.SetActingSurface(ctx, string(audit.SurfaceSystem))

	queries := orgrepo.New(tx)
	if err := queries.LockUserOrganizationCap(ctx, membership.WorkOSUserID); err != nil {
		return false, fmt.Errorf("lock user organization cap: %w", err)
	}

	disabled, err := queries.DisableOrganizationOverUserCap(ctx, orgrepo.DisableOrganizationOverUserCapParams{
		OrganizationID:   membership.OrganizationID,
		UserID:           conv.ToPGTextEmpty(membership.UserID),
		WorkosUserID:     conv.ToPGText(membership.WorkOSUserID),
		MaxOrganizations: MaxOrganizationsPerUser,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("disable organization over user cap: %w", err)
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
		return false, fmt.Errorf("audit organization cap: %w", err)
	}

	return true, nil
}
