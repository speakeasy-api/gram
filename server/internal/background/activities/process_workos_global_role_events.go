package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/workos/workos-go/v6/pkg/events"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

const workosGlobalRoleEventsPageSize = 100

// WorkosGlobalRoleCursorKey is the workos_organization_id value used to store
// the singleton cursor for environment-level role events in workos_organization_syncs.
// Empty string is intentional: role.* events have no organization scope.
const WorkosGlobalRoleCursorKey = ""

type ProcessWorkOSGlobalRoleEventsParams struct {
	SinceEventID *string `json:"since_event_id,omitempty"`
}

type ProcessWorkOSGlobalRoleEventsResult struct {
	SinceEventID string `json:"since_event_id"`
	LastEventID  string `json:"last_event_id"`
	HasMore      bool   `json:"has_more"`
}

// ProcessWorkOSGlobalRoleEvents processes WorkOS role.* events — environment-
// level roles that are not scoped to any organization. Org-scoped role events
// flow through ProcessWorkOSOrganizationEvents and are not handled here.
type ProcessWorkOSGlobalRoleEvents struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	workosClient WorkOSClient
}

func NewProcessWorkOSGlobalRoleEvents(logger *slog.Logger, db *pgxpool.Pool, workosClient WorkOSClient) *ProcessWorkOSGlobalRoleEvents {
	return &ProcessWorkOSGlobalRoleEvents{
		db:           db,
		logger:       logger,
		workosClient: workosClient,
	}
}

func (p *ProcessWorkOSGlobalRoleEvents) Do(ctx context.Context, params ProcessWorkOSGlobalRoleEventsParams) (*ProcessWorkOSGlobalRoleEventsResult, error) {
	sinceEventID := conv.PtrValOr(params.SinceEventID, "")
	if sinceEventID == "" {
		cursor, err := workosrepo.New(p.db).GetOrganizationSyncLastEventID(ctx, WorkosGlobalRoleCursorKey)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "get global role sync cursor").Log(ctx, p.logger)
		default:
			sinceEventID = cursor
		}
	}

	resp, err := p.workosClient.ListEvents(ctx, events.ListEventsOpts{
		Events: []string{
			string(workos.EventKindRoleCreated),
			string(workos.EventKindRoleUpdated),
			string(workos.EventKindRoleDeleted),
		},
		Limit:          workosGlobalRoleEventsPageSize,
		After:          sinceEventID,
		OrganizationId: "",
		RangeStart:     "",
		RangeEnd:       "",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list WorkOS global role events").Log(ctx, p.logger)
	}

	lastEventID, err := p.handlePage(ctx, resp.Data)
	if err != nil {
		return nil, err
	}

	return &ProcessWorkOSGlobalRoleEventsResult{
		SinceEventID: sinceEventID,
		LastEventID:  lastEventID,
		HasMore:      len(resp.Data) == workosGlobalRoleEventsPageSize,
	}, nil
}

func (p *ProcessWorkOSGlobalRoleEvents) handlePage(ctx context.Context, page []events.Event) (string, error) {
	var lastEventID string
	for _, event := range page {
		eventLogger := p.logger.With(
			attr.SlogWorkOSEventID(event.ID),
			attr.SlogWorkOSEventType(event.Event),
		)

		eventID, err := p.handleEvent(ctx, eventLogger, event)
		if err != nil {
			return lastEventID, oops.E(oops.CodeUnexpected, err, "handle WorkOS global role event").Log(ctx, eventLogger)
		}
		if eventID != "" {
			lastEventID = eventID
		}
	}
	return lastEventID, nil
}

func (p *ProcessWorkOSGlobalRoleEvents) handleEvent(ctx context.Context, logger *slog.Logger, event events.Event) (string, error) {
	var payload workosRoleEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return "", oops.Permanent(fmt.Errorf("unmarshal role event payload: %w", err))
	}

	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	switch workos.EventKind(event.Event) {
	case workos.EventKindRoleCreated, workos.EventKindRoleUpdated:
		if err := upsertGlobalRole(ctx, dbtx, event, payload); err != nil {
			return "", err
		}
	case workos.EventKindRoleDeleted:
		if err := deleteGlobalRole(ctx, dbtx, event, payload); err != nil {
			return "", err
		}
	default:
		logger.DebugContext(ctx, "unhandled global role event type, advancing cursor")
	}

	if _, err := workosrepo.New(dbtx).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{
		WorkosOrganizationID: WorkosGlobalRoleCursorKey,
		LastEventID:          event.ID,
	}); err != nil {
		return "", fmt.Errorf("set global role sync cursor: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	return event.ID, nil
}

func upsertGlobalRole(ctx context.Context, dbtx database.DBTX, event events.Event, payload workosRoleEventPayload) error {
	repo := accessrepo.New(dbtx)

	existing, err := repo.GetGlobalRoleBySlug(ctx, payload.Slug)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get global role %q: %w", payload.Slug, err)
	}

	var lastEventID *string
	if existing.WorkosLastEventID.Valid {
		lastEventID = &existing.WorkosLastEventID.String
	}
	rowUpdatedAt := existing.WorkosUpdatedAt.Time
	if !ShouldProcessEvent(lastEventID, &rowUpdatedAt, event.ID, payload.UpdatedAt) {
		return nil
	}

	if err := repo.UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
		WorkosSlug:        payload.Slug,
		WorkosName:        payload.Name,
		WorkosDescription: conv.ToPGTextEmpty(payload.Description),
		WorkosCreatedAt:   conv.ToPGTimestamptz(payload.CreatedAt),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(payload.UpdatedAt),
		WorkosLastEventID: conv.ToPGText(event.ID),
	}); err != nil {
		return fmt.Errorf("upsert global role %q: %w", payload.Slug, err)
	}

	return nil
}

func deleteGlobalRole(ctx context.Context, dbtx database.DBTX, event events.Event, payload workosRoleEventPayload) error {
	deletedAt := payload.DeletedAt
	if deletedAt == nil || deletedAt.IsZero() {
		deletedAt = &event.CreatedAt
	}

	repo := accessrepo.New(dbtx)
	existing, err := repo.GetGlobalRoleBySlug(ctx, payload.Slug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("get global role %q: %w", payload.Slug, err)
	}

	var lastEventID *string
	if existing.WorkosLastEventID.Valid {
		lastEventID = &existing.WorkosLastEventID.String
	}
	rowUpdatedAt := existing.WorkosUpdatedAt.Time
	if !ShouldProcessEvent(lastEventID, &rowUpdatedAt, event.ID, payload.UpdatedAt) {
		return nil
	}

	if _, err := repo.MarkGlobalRoleDeleted(ctx, accessrepo.MarkGlobalRoleDeletedParams{
		WorkosSlug:        payload.Slug,
		WorkosDeletedAt:   conv.ToPGTimestamptz(*deletedAt),
		WorkosLastEventID: conv.ToPGText(event.ID),
	}); err != nil {
		return fmt.Errorf("mark global role %q deleted: %w", payload.Slug, err)
	}
	return nil
}
