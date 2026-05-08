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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const workosMembershipEventsPageSize = 100

type ProcessWorkOSMembershipEventsParams struct {
	// SinceEventID lets a caller override the DB cursor for this run. Workflow
	// triggers pass nil and let the activity load the singleton cursor from
	// `workos_user_syncs`.
	SinceEventID *string `json:"since_event_id,omitempty"`
}

type ProcessWorkOSMembershipEventsResult struct {
	SinceEventID string `json:"since_event_id"`
	LastEventID  string `json:"last_event_id"`
	HasMore      bool   `json:"has_more"`
}

// ProcessWorkOSMembershipEvents processes the global organization_membership.*
// stream. Membership events are not filtered by WorkOS organization because
// WorkOS users may move across orgs and the cursor is intentionally singleton.
type ProcessWorkOSMembershipEvents struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	eventsClient EventsLister
}

func NewProcessWorkOSMembershipEvents(logger *slog.Logger, db *pgxpool.Pool, eventsClient EventsLister) *ProcessWorkOSMembershipEvents {
	return &ProcessWorkOSMembershipEvents{
		db:           db,
		logger:       logger,
		eventsClient: eventsClient,
	}
}

func (p *ProcessWorkOSMembershipEvents) Do(ctx context.Context, params ProcessWorkOSMembershipEventsParams) (*ProcessWorkOSMembershipEventsResult, error) {
	logger := p.logger

	sinceEventID := conv.PtrValOr(params.SinceEventID, "")
	if sinceEventID == "" {
		cursor, err := workosrepo.New(p.db).GetUserSyncLastEventID(ctx)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// No cursor yet: full sync from the beginning.
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "get user sync last event ID").Log(ctx, logger)
		default:
			sinceEventID = cursor
		}
	}

	resp, err := p.eventsClient.ListEvents(ctx, events.ListEventsOpts{
		Events: []string{
			string(workos.EventKindOrganizationMembershipCreated),
			string(workos.EventKindOrganizationMembershipDeleted),
			string(workos.EventKindOrganizationMembershipUpdated),
		},
		Limit:          workosMembershipEventsPageSize,
		After:          sinceEventID,
		OrganizationId: "",
		RangeStart:     "",
		RangeEnd:       "",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list WorkOS membership events").Log(ctx, logger)
	}

	lastEventID, err := p.handlePage(ctx, logger, resp.Data)
	if err != nil {
		return nil, err
	}

	return &ProcessWorkOSMembershipEventsResult{
		SinceEventID: sinceEventID,
		LastEventID:  lastEventID,
		HasMore:      len(resp.Data) == workosMembershipEventsPageSize,
	}, nil
}

func (p *ProcessWorkOSMembershipEvents) handlePage(ctx context.Context, logger *slog.Logger, page []events.Event) (string, error) {
	var lastEventID string
	for _, event := range page {
		eventLogger := logger.With(
			attr.SlogWorkOSEventID(event.ID),
			attr.SlogWorkOSEventType(event.Event),
		)

		eventID, err := p.handleEvent(ctx, eventLogger, event)
		if err != nil {
			return lastEventID, oops.E(oops.CodeUnexpected, err, "handle WorkOS membership event").Log(ctx, eventLogger)
		}
		if eventID != "" {
			lastEventID = eventID
		}
	}

	return lastEventID, nil
}

func (p *ProcessWorkOSMembershipEvents) handleEvent(ctx context.Context, logger *slog.Logger, event events.Event) (string, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := handleMembershipEvent(ctx, logger, dbtx, event); err != nil {
		return "", err
	}

	if _, err := workosrepo.New(dbtx).SetUserSyncLastEventID(ctx, event.ID); err != nil {
		return "", fmt.Errorf("set user sync last event ID: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	return event.ID, nil
}

func handleMembershipEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	switch event.Event {
	case string(workos.EventKindOrganizationMembershipCreated), string(workos.EventKindOrganizationMembershipUpdated):
		return handleOrganizationMembershipUpsert(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationMembershipDeleted):
		return handleOrganizationMembershipDeleted(ctx, logger, dbtx, event)
	}

	return oops.Permanent(fmt.Errorf("unhandled workos membership event type: %s", event.Event))
}

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
