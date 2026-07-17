package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/workos/workos-go/v6/pkg/events"
	"github.com/workos/workos-go/v6/pkg/usermanagement"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type workosMembershipRole struct {
	Slug string `json:"slug"`
}

type workosMembershipEventPayload struct {
	ID             string                 `json:"id"`
	Object         string                 `json:"object"`
	OrganizationID string                 `json:"organization_id"`
	UserID         string                 `json:"user_id"`
	RoleSlug       string                 `json:"role_slug"`
	Role           workosMembershipRole   `json:"role"`
	Roles          []workosMembershipRole `json:"roles"`
	Status         string                 `json:"status"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// handleOrganizationMembershipEvent applies an organization_membership.*
// event. A delete event or an update carrying status=inactive deprovisions
// the user's organization access — SCIM deactivation (e.g. suspending a user
// in the IdP) surfaces as organization_membership.updated with
// status=inactive, not as a delete event. Anything else upserts, and a later
// update with status=active restores access through that path.
func handleOrganizationMembershipEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) (postCommitEffects, error) {
	var none postCommitEffects

	payload, err := decodeWorkOSMembershipPayload(event)
	if err != nil {
		return none, err
	}
	logger = logger.With(
		attr.SlogWorkOSOrganizationID(payload.OrganizationID),
		attr.SlogWorkOSUserID(payload.UserID),
	)

	org, err := orgrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "skipping membership event for unknown organization")
		return none, nil
	case err != nil:
		return none, fmt.Errorf("get organization by workos id %q: %w", payload.OrganizationID, err)
	}

	gramUserID, err := usersrepo.New(dbtx).GetUserIDByWorkosID(ctx, conv.ToPGText(payload.UserID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return none, fmt.Errorf("get user by workos id %q: %w", payload.UserID, err)
	}

	lastEventID, rowUpdatedAt, err := getMembershipRelationshipCursor(ctx, orgrepo.New(dbtx), org.ID, gramUserID, payload.ID)
	if err != nil {
		return none, err
	}
	if !ShouldProcessEvent(lastEventID, rowUpdatedAt, event.ID, payload.UpdatedAt) {
		return none, nil
	}

	deleted := workos.EventKind(event.Event) == workos.EventKindOrganizationMembershipDeleted
	if deleted || payload.Status == string(usermanagement.Inactive) {
		return deprovisionOrganizationAccess(ctx, dbtx, deprovisionOrganizationAccessParams{
			organizationID:     org.ID,
			gramUserID:         gramUserID,
			workosUserID:       payload.UserID,
			workosMembershipID: payload.ID,
			eventID:            event.ID,
			eventUpdatedAt:     payload.UpdatedAt,
		})
	}

	return none, upsertOrganizationMembership(ctx, dbtx, org.ID, gramUserID, event, payload)
}

// upsertOrganizationMembership records an active membership and declaratively
// syncs the member's WorkOS role assignments. Caller owns the
// ShouldProcessEvent guard.
func upsertOrganizationMembership(ctx context.Context, dbtx database.DBTX, organizationID string, gramUserID string, event events.Event, payload workosMembershipEventPayload) error {
	if err := orgrepo.New(dbtx).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGTextEmpty(gramUserID),
		WorkosUserID:       conv.ToPGText(payload.UserID),
		WorkosMembershipID: conv.ToPGText(payload.ID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(payload.UpdatedAt),
		WorkosLastEventID:  conv.ToPGText(event.ID),
	}); err != nil {
		return fmt.Errorf("upsert organization membership %q: %w", payload.ID, err)
	}

	if err := orgrepo.New(dbtx).SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     organizationID,
		WorkosUserID:       payload.UserID,
		UserID:             conv.ToPGTextEmpty(gramUserID),
		WorkosMembershipID: conv.ToPGText(payload.ID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(payload.UpdatedAt),
		WorkosLastEventID:  conv.ToPGText(event.ID),
		WorkosRoleSlugs:    membershipRoleSlugs(payload),
	}); err != nil {
		return fmt.Errorf("sync organization role assignments for membership %q: %w", payload.ID, err)
	}

	return nil
}

func getMembershipRelationshipCursor(ctx context.Context, repo *orgrepo.Queries, organizationID, gramUserID, workosMembershipID string) (*string, *time.Time, error) {
	existing, err := repo.GetRelationshipByMembershipID(ctx, conv.ToPGText(workosMembershipID))
	if errors.Is(err, pgx.ErrNoRows) && gramUserID != "" {
		existing, err = repo.GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
			OrganizationID: organizationID,
			UserID:         conv.ToPGText(gramUserID),
		})
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, nil
	case err != nil:
		return nil, nil, fmt.Errorf("get organization membership %q: %w", workosMembershipID, err)
	}

	var lastEventID *string
	if existing.WorkosLastEventID.Valid {
		lastEventID = &existing.WorkosLastEventID.String
	}
	var rowUpdatedAt *time.Time
	if existing.WorkosUpdatedAt.Valid {
		rowUpdatedAt = &existing.WorkosUpdatedAt.Time
	}

	return lastEventID, rowUpdatedAt, nil
}

func decodeWorkOSMembershipPayload(event events.Event) (workosMembershipEventPayload, error) {
	var payload workosMembershipEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return payload, oops.Permanent(fmt.Errorf("unmarshal organization membership event payload: %w", err))
	}
	if payload.ID == "" || payload.OrganizationID == "" || payload.UserID == "" || payload.UpdatedAt.IsZero() {
		return payload, oops.Permanent(fmt.Errorf("invalid organization membership event payload"))
	}
	return payload, nil
}

func membershipRoleSlugs(payload workosMembershipEventPayload) []string {
	roleSlugs := make([]string, 0, len(payload.Roles)+2)
	for _, role := range payload.Roles {
		if role.Slug != "" {
			roleSlugs = append(roleSlugs, role.Slug)
		}
	}
	if payload.Role.Slug != "" {
		roleSlugs = append(roleSlugs, payload.Role.Slug)
	}
	if payload.RoleSlug != "" {
		roleSlugs = append(roleSlugs, payload.RoleSlug)
	}

	slices.Sort(roleSlugs)
	return slices.Compact(roleSlugs)
}
