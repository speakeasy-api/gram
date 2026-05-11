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

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
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
	UpdatedAt      time.Time              `json:"updated_at"`
}

func handleOrganizationMembershipUpsert(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	payload, err := decodeWorkOSMembershipPayload(event)
	if err != nil {
		return err
	}
	logger = logger.With(
		attr.SlogWorkOSOrganizationID(payload.OrganizationID),
		attr.SlogWorkOSUserID(payload.UserID),
	)

	org, err := orgrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "skipping membership event for unknown organization")
		return nil
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.OrganizationID, err)
	}

	gramUserID, err := usersrepo.New(dbtx).GetUserIDByWorkosID(ctx, conv.ToPGText(payload.UserID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get user by workos id %q: %w", payload.UserID, err)
	}

	if gramUserID != "" {
		existing, err := orgrepo.New(dbtx).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
			OrganizationID: org.ID,
			UserID:         gramUserID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return fmt.Errorf("get organization membership %q: %w", payload.ID, err)
		}
		var lastEventID *string
		if existing.WorkosLastEventID.Valid {
			lastEventID = &existing.WorkosLastEventID.String
		}
		var rowUpdatedAt *time.Time
		if existing.WorkosUpdatedAt.Valid {
			rowUpdatedAt = &existing.WorkosUpdatedAt.Time
		}
		if !ShouldProcessEvent(lastEventID, rowUpdatedAt, event.ID, payload.UpdatedAt) {
			return nil
		}
		if err := orgrepo.New(dbtx).UpsertOrganizationUserRelationshipFromWorkOS(ctx, orgrepo.UpsertOrganizationUserRelationshipFromWorkOSParams{
			OrganizationID:     org.ID,
			UserID:             gramUserID,
			WorkosMembershipID: conv.ToPGText(payload.ID),
			WorkosUpdatedAt:    conv.ToPGTimestamptz(payload.UpdatedAt),
			WorkosLastEventID:  conv.ToPGText(event.ID),
		}); err != nil {
			return fmt.Errorf("upsert organization membership %q: %w", payload.ID, err)
		}
	}

	if err := orgrepo.New(dbtx).SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     org.ID,
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

func handleOrganizationMembershipDeleted(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	payload, err := decodeWorkOSMembershipPayload(event)
	if err != nil {
		return err
	}
	logger = logger.With(
		attr.SlogWorkOSOrganizationID(payload.OrganizationID),
		attr.SlogWorkOSUserID(payload.UserID),
	)

	org, err := orgrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "skipping membership delete for unknown organization")
		return nil
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.OrganizationID, err)
	}

	gramUserID, err := usersrepo.New(dbtx).GetUserIDByWorkosID(ctx, conv.ToPGText(payload.UserID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get user by workos id %q: %w", payload.UserID, err)
	}

	if gramUserID != "" {
		existing, err := orgrepo.New(dbtx).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
			OrganizationID: org.ID,
			UserID:         gramUserID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return fmt.Errorf("get organization membership %q: %w", payload.ID, err)
		}
		var lastEventID *string
		if existing.WorkosLastEventID.Valid {
			lastEventID = &existing.WorkosLastEventID.String
		}
		var rowUpdatedAt *time.Time
		if existing.WorkosUpdatedAt.Valid {
			rowUpdatedAt = &existing.WorkosUpdatedAt.Time
		}
		if !ShouldProcessEvent(lastEventID, rowUpdatedAt, event.ID, payload.UpdatedAt) {
			return nil
		}
		if err := orgrepo.New(dbtx).MarkOrganizationUserRelationshipAsDeleted(ctx, orgrepo.MarkOrganizationUserRelationshipAsDeletedParams{
			OrganizationID:     org.ID,
			UserID:             gramUserID,
			WorkosMembershipID: conv.ToPGText(payload.ID),
			WorkosUpdatedAt:    conv.ToPGTimestamptz(payload.UpdatedAt),
			WorkosLastEventID:  conv.ToPGText(event.ID),
		}); err != nil {
			return fmt.Errorf("mark organization membership %q deleted: %w", payload.ID, err)
		}
	}

	if err := orgrepo.New(dbtx).DeleteOrganizationRoleAssignmentsByWorkosUser(ctx, orgrepo.DeleteOrganizationRoleAssignmentsByWorkosUserParams{
		OrganizationID: org.ID,
		WorkosUserID:   payload.UserID,
	}); err != nil {
		return fmt.Errorf("delete role assignments for workos user %q: %w", payload.UserID, err)
	}

	return nil
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
