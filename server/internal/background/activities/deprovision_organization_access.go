package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// deprovisionOrganizationAccessParams identifies whose organization access to
// revoke and the WorkOS event driving the change. Callers own the
// ShouldProcessEvent guard; this routine only applies the teardown.
type deprovisionOrganizationAccessParams struct {
	organizationID string
	// gramUserID may be empty when the WorkOS user has no local Gram user.
	gramUserID string
	// workosUserID may be empty (e.g. a relationship row created before the
	// user ever logged in); it is then resolved from the users row so role
	// assignments can still be revoked.
	workosUserID string
	// workosMembershipID may be empty for triggers that do not carry a
	// membership (directory user deactivation).
	workosMembershipID string
	eventID            string
	eventUpdatedAt     time.Time
}

// deprovisionOrganizationAccess is the single teardown routine for every
// WorkOS deprovisioning trigger: organization_membership deletion, membership
// status=inactive, and directory user deactivation. It marks the organization
// relationship and the user's role assignments deleted, and requests a
// user-info cache invalidation so org-access checks observe the change
// without waiting out the cache TTL.
func deprovisionOrganizationAccess(ctx context.Context, dbtx database.DBTX, p deprovisionOrganizationAccessParams) (postCommitEffects, error) {
	var effects postCommitEffects

	repo := orgrepo.New(dbtx)
	if err := repo.MarkWorkOSMembershipDeleted(ctx, orgrepo.MarkWorkOSMembershipDeletedParams{
		OrganizationID:     p.organizationID,
		UserID:             conv.ToPGTextEmpty(p.gramUserID),
		WorkosUserID:       conv.ToPGTextEmpty(p.workosUserID),
		WorkosMembershipID: conv.ToPGTextEmpty(p.workosMembershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(p.eventUpdatedAt),
		WorkosLastEventID:  conv.ToPGText(p.eventID),
	}); err != nil {
		return effects, fmt.Errorf("mark organization membership deleted for workos user %q: %w", p.workosUserID, err)
	}

	workosUserID := p.workosUserID
	if workosUserID == "" && p.gramUserID != "" {
		user, err := usersrepo.New(dbtx).GetUser(ctx, p.gramUserID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return effects, fmt.Errorf("get user %q for deprovisioning: %w", p.gramUserID, err)
		case user.WorkosID.Valid:
			workosUserID = user.WorkosID.String
		}
	}
	if workosUserID != "" {
		if err := repo.MarkRoleAssignmentsDeleted(ctx, orgrepo.MarkRoleAssignmentsDeletedParams{
			OrganizationID:    p.organizationID,
			WorkosUserID:      workosUserID,
			WorkosUpdatedAt:   conv.ToPGTimestamptz(p.eventUpdatedAt),
			WorkosLastEventID: conv.ToPGText(p.eventID),
		}); err != nil {
			return effects, fmt.Errorf("mark role assignments deleted for workos user %q: %w", workosUserID, err)
		}
	}

	effects.invalidateUserInfoCacheUserID = p.gramUserID
	return effects, nil
}
