package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agentownership"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/signins"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var deprovisionActor = signins.Actor{Principal: urn.NewSystemPrincipal("workos-deprovision"), DisplayName: nil}

// revokeDeprovisionedSignIns revokes the deprovisioned user's sign-ins and
// grants in the event's transaction when enforcement is on. Lock order: the
// membership rows above, then the user's sign-in locks, as in RevokeUser.
func revokeDeprovisionedSignIns(ctx context.Context, dbtx pgx.Tx, r signInRevocation, effects *postCommitEffects) error {
	organizationIDs := []string{r.organizationID}
	revoked, err := signins.RevokeUser(ctx, dbtx, organizationIDs, r.userID, signins.RevokeReasonDeprovisioned)
	if err != nil {
		return fmt.Errorf("revoke sign-ins for user %q: %w", r.userID, err)
	}
	if err := signins.AuditRevocation(ctx, dbtx, audit.NewLogger(), deprovisionActor, r.userID, "", signins.RevokeReasonDeprovisioned, organizationIDs, revoked); err != nil {
		return fmt.Errorf("audit sign-in revocation for user %q: %w", r.userID, err)
	}
	effects.revokedSignIns = revoked.SignIns()
	effects.revocationOrganizationID = r.organizationID
	effects.revocationDeliveries = revoked.DeliveryIDs
	return nil
}

// deprovisionOrganizationAccessParams identifies whose organization access to
// revoke and the WorkOS event driving the change. Callers own the
// ShouldProcessEvent guard; this routine only applies the teardown.
type deprovisionOrganizationAccessParams struct {
	organizationID string
	// gramUserID may be empty when the WorkOS user has no local Speakeasy user.
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
	ownerLossReason    agentownership.OwnerReassignmentReason
}

// deprovisionOrganizationAccess is the single teardown routine for every
// WorkOS deprovisioning trigger: organization_membership deletion, membership
// status=inactive, and directory user deactivation. It marks the organization
// relationship and the user's role assignments deleted, and requests a
// user-info cache invalidation so org-access checks observe the change
// without waiting out the cache TTL, plus an identity map refresh so the
// ClickHouse fold stops resolving the departed member's emails before the
// sync schedule's next tick.
func deprovisionOrganizationAccess(ctx context.Context, dbtx pgx.Tx, p deprovisionOrganizationAccessParams) (postCommitEffects, error) {
	var effects postCommitEffects

	repo := orgrepo.New(dbtx)
	ownerUserIDs, err := repo.MarkWorkOSMembershipDeleted(ctx, orgrepo.MarkWorkOSMembershipDeletedParams{
		OrganizationID:     p.organizationID,
		UserID:             conv.ToPGTextEmpty(p.gramUserID),
		WorkosUserID:       conv.ToPGTextEmpty(p.workosUserID),
		WorkosMembershipID: conv.ToPGTextEmpty(p.workosMembershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(p.eventUpdatedAt),
		WorkosLastEventID:  conv.ToPGText(p.eventID),
	})
	if err != nil {
		return effects, fmt.Errorf("mark organization membership deleted for workos user %q: %w", p.workosUserID, err)
	}
	owners := make(map[string]struct{}, len(ownerUserIDs)+1)
	if p.gramUserID != "" {
		owners[p.gramUserID] = struct{}{}
	}
	for _, candidate := range ownerUserIDs {
		if candidate.Valid && candidate.String != "" {
			owners[candidate.String] = struct{}{}
		}
	}
	for ownerUserID := range owners {
		if err := agentownership.LatchOwnerLossByMembership(
			ctx, dbtx, p.organizationID, ownerUserID, p.ownerLossReason, agentownership.SystemActor, nil,
		); err != nil {
			return effects, fmt.Errorf("latch agent owner membership loss: %w", err)
		}
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

	if p.gramUserID != "" {
		effects.revokeSignIns = &signInRevocation{organizationID: p.organizationID, userID: p.gramUserID}
	}

	effects.invalidateUserInfoCacheUserID = p.gramUserID
	effects.refreshIdentityMap = true
	return effects, nil
}
