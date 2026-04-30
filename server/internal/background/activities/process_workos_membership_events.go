package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/workos/workos-go/v6/pkg/events"
)

type ProcessWorkOSMembershipEventsParams struct {
	SinceEventID *string `json:"since_event_id,omitempty"`
}

type ProcessWorkOSMembershipEventsResult struct {
	SinceEventID string `json:"since_event_id"`
	LastEventID  string `json:"last_event_id"`
	HasMore      bool   `json:"has_more"`
}

// ProcessWorkOSMembershipEvents processes the global stream of organization_membership.* events.
// Cursor lives in workos_user_syncs (singleton). No OrganizationId filter — all membership events
// across all orgs flow through this single workflow. Decoupled from per-org event processing so a
// membership event for an unknown org can still be stored (with workos_org_id) and reconciled later.
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
			// no cursor yet, full sync from beginning
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get user sync last event ID").Log(ctx, logger)
		default:
			sinceEventID = cursor
		}
	}

	pageSize := 100

	options := events.ListEventsOpts{
		Events: stringifyEventKinds(
			workos.EventKindOrganizationMembershipCreated,
			workos.EventKindOrganizationMembershipDeleted,
			workos.EventKindOrganizationMembershipUpdated,
		),
		Limit:      pageSize,
		After:      sinceEventID,
		RangeStart: "",
		RangeEnd:   "",
	}

	resp, err := p.eventsClient.ListEvents(ctx, options)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list WorkOS membership events").Log(ctx, logger)
	}

	lastEventID, err := p.handlePage(ctx, logger, resp.Data)
	if err != nil {
		return nil, err
	}

	return &ProcessWorkOSMembershipEventsResult{
		SinceEventID: sinceEventID,
		LastEventID:  lastEventID,
		HasMore:      len(resp.Data) == pageSize,
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
			return lastEventID, oops.E(oops.CodeUnexpected, err, "failed to handle WorkOS membership event").Log(ctx, eventLogger)
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
	case string(workos.EventKindOrganizationMembershipCreated),
		string(workos.EventKindOrganizationMembershipUpdated):
		return handleOrganizationMembershipUpsert(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationMembershipDeleted):
		return handleOrganizationMembershipDeleted(ctx, logger, dbtx, event)
	}

	return oops.Permanent(fmt.Errorf("unhandled workos membership event: %s", event.Event))
}

type workosMembershipRole struct {
	Slug string `json:"slug"`
}

type workosMembershipEvent struct {
	ID             string                 `json:"id"`
	Object         string                 `json:"object"`
	OrganizationID string                 `json:"organization_id"`
	UserID         string                 `json:"user_id"`
	RoleSlug       string                 `json:"role_slug"`
	Role           workosMembershipRole   `json:"role"`
	Roles          []workosMembershipRole `json:"roles"`
	Status         string                 `json:"status"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

func handleOrganizationMembershipUpsert(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosMembershipEvent
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal organization membership event: %w", err)
	}

	organizationID, err := orgrepo.New(dbtx).GetOrganizationIDByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "skipping membership event for unknown organization", attr.SlogWorkOSOrganizationID(payload.OrganizationID))
		return nil
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.OrganizationID, err)
	}

	gramUserID, err := usersrepo.New(dbtx).GetUserIDByWorkosID(ctx, conv.ToPGText(payload.UserID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "user not yet in gram, storing role assignments without user link", attr.SlogWorkOSUserID(payload.UserID))
	case err != nil:
		return fmt.Errorf("get user by workos id %q: %w", payload.UserID, err)
	default:
		if err := orgrepo.New(dbtx).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
			OrganizationID:     organizationID,
			UserID:             gramUserID,
			WorkosMembershipID: conv.ToPGText(payload.ID),
		}); err != nil {
			return fmt.Errorf("upsert organization membership %q: %w", payload.ID, err)
		}
	}

	// WorkOS membership events expose roles in three shapes depending on event version:
	// - Roles []Role (newer schema, preferred)
	// - Role   Role  (single-role fallback)
	// - RoleSlug string (legacy flat field)
	roleSlugs := make([]string, 0, len(payload.Roles))
	for _, r := range payload.Roles {
		if r.Slug != "" {
			roleSlugs = append(roleSlugs, r.Slug)
		}
	}
	if len(roleSlugs) == 0 && payload.Role.Slug != "" {
		roleSlugs = append(roleSlugs, payload.Role.Slug)
	}
	if len(roleSlugs) == 0 && payload.RoleSlug != "" {
		roleSlugs = append(roleSlugs, payload.RoleSlug)
	}

	roleSlugs = dedupeStrings(roleSlugs)

	userIDParam := conv.ToPGTextEmpty(gramUserID)
	if err := orgrepo.New(dbtx).SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     organizationID,
		WorkosUserID:       payload.UserID,
		UserID:             userIDParam,
		WorkosRoleSlugs:    roleSlugs,
		WorkosMembershipID: conv.ToPGText(payload.ID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(payload.UpdatedAt),
		WorkosLastEventID:  conv.ToPGText(event.ID),
	}); err != nil {
		return fmt.Errorf("sync organization user role assignments for membership %q: %w", payload.ID, err)
	}

	return nil
}

func handleOrganizationMembershipDeleted(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosMembershipEvent
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal organization membership delete event: %w", err)
	}

	organizationID, err := orgrepo.New(dbtx).GetOrganizationIDByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "skipping membership delete for unknown organization", attr.SlogWorkOSOrganizationID(payload.OrganizationID))
		return nil
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.OrganizationID, err)
	}

	if err := orgrepo.New(dbtx).DeleteOrganizationRoleAssignmentsByWorkosUser(ctx, orgrepo.DeleteOrganizationRoleAssignmentsByWorkosUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   payload.UserID,
	}); err != nil {
		return fmt.Errorf("delete role assignments for workos user %q: %w", payload.UserID, err)
	}

	gramUserID, err := usersrepo.New(dbtx).GetUserIDByWorkosID(ctx, conv.ToPGText(payload.UserID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "skipping relationship delete for unknown user", attr.SlogWorkOSUserID(payload.UserID))
		return nil
	case err != nil:
		return fmt.Errorf("get user by workos id %q: %w", payload.UserID, err)
	}

	if err := orgrepo.New(dbtx).DeleteOrganizationUserRelationship(ctx, orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         gramUserID,
	}); err != nil {
		return fmt.Errorf("delete organization membership %q: %w", payload.ID, err)
	}

	return nil
}
