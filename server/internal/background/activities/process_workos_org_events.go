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
	"github.com/workos/workos-go/v6/pkg/events"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	workosOrgEventsPageSize   = 100
	workosRoleTypeEnvironment = "EnvironmentRole"
)

// EventsLister is the subset of the WorkOS events client used by this activity.
type EventsLister interface {
	ListEvents(ctx context.Context, opts events.ListEventsOpts) (events.ListEventsResponse, error)
}

type ProcessWorkOSOrganizationEventsParams struct {
	WorkOSOrganizationID string `json:"workos_organization_id,omitempty"`
	// SinceEventID lets a caller override the DB cursor for this run. Workflow
	// triggers always pass nil and let the activity load the cursor from
	// `workos_organization_syncs`. The override exists for manual reconcile /
	// backfill paths (future PRs) that want to replay from a specific event.
	SinceEventID *string `json:"since_event_id,omitempty"`
}

type ProcessWorkOSOrganizationEventsResult struct {
	SinceEventID string `json:"since_event_id"`
	LastEventID  string `json:"last_event_id"`
	HasMore      bool   `json:"has_more"`
}

// ProcessWorkOSOrganizationEvents pages through WorkOS organization-scoped events
// since the stored cursor, advancing the cursor as it goes. Event handling itself
// (upserting org metadata, role rows, etc.) is wired in a follow-up PR — for now
// the activity only advances the cursor so subsequent runs pick up where this
// left off.
type ProcessWorkOSOrganizationEvents struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	eventsClient EventsLister
}

func NewProcessWorkOSOrganizationEvents(logger *slog.Logger, db *pgxpool.Pool, eventsClient EventsLister) *ProcessWorkOSOrganizationEvents {
	return &ProcessWorkOSOrganizationEvents{
		db:           db,
		logger:       logger,
		eventsClient: eventsClient,
	}
}

func (p *ProcessWorkOSOrganizationEvents) Do(ctx context.Context, params ProcessWorkOSOrganizationEventsParams) (*ProcessWorkOSOrganizationEventsResult, error) {
	workOSOrgID := params.WorkOSOrganizationID
	logger := p.logger.With(attr.SlogWorkOSOrganizationID(workOSOrgID))

	sinceEventID := conv.PtrValOr(params.SinceEventID, "")
	// we don't have information in params on what the last processed event was.
	// Check the database for the last ID.
	if sinceEventID == "" {
		cursor, err := workosrepo.New(p.db).GetOrganizationSyncLastEventID(ctx, workOSOrgID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// No cursor yet — full sync from the beginning.
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get organization sync last event ID").Log(ctx, logger)
		default:
			sinceEventID = cursor
		}
	}

	resp, err := p.eventsClient.ListEvents(ctx, events.ListEventsOpts{
		Events: []string{
			string(workos.EventKindOrganizationCreated),
			string(workos.EventKindOrganizationUpdated),
			string(workos.EventKindOrganizationDeleted),

			string(workos.EventKindOrganizationRoleCreated),
			string(workos.EventKindOrganizationRoleDeleted),
			string(workos.EventKindOrganizationRoleUpdated),
		},
		Limit:          workosOrgEventsPageSize,
		After:          sinceEventID,
		OrganizationId: workOSOrgID,
		RangeStart:     "",
		RangeEnd:       "",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list WorkOS events").Log(ctx, logger)
	}

	lastEventID, err := p.handlePage(ctx, logger, workOSOrgID, resp.Data)
	if err != nil {
		return nil, err
	}

	return &ProcessWorkOSOrganizationEventsResult{
		SinceEventID: sinceEventID,
		LastEventID:  lastEventID,
		// The WorkOS SDK's ListEventsResponse does not surface a "has more"
		// flag, so a full page is the only signal we have. Trade-off: when the
		// final page is exactly workosOrgEventsPageSize items, we'll trigger
		// one extra run that returns an empty page. The empty-page path is a
		// no-op so this is harmless beyond an extra trace.
		HasMore: len(resp.Data) == workosOrgEventsPageSize,
	}, nil
}

type workosOrgEvent struct {
	ID             string `json:"id"`
	Object         string `json:"object"`
	OrganizationID string `json:"organization_id"`
}

func (p *ProcessWorkOSOrganizationEvents) handlePage(ctx context.Context, logger *slog.Logger, workosOrgID string, page []events.Event) (string, error) {
	var lastEventID string
	for _, event := range page {
		eventLogger := logger.With(
			attr.SlogWorkOSEventID(event.ID),
			attr.SlogWorkOSEventType(event.Event),
		)

		var orgEvent workosOrgEvent
		if err := json.Unmarshal(event.Data, &orgEvent); err != nil {
			return lastEventID, oops.E(oops.CodeUnexpected, err, "failed to unmarshal workos organization event data").Log(ctx, eventLogger)
		}

		// Resolve the WorkOS organization ID from the payload for logging.
		// organization.* events carry it on .id; organization_role.* events
		// carry it on .organization_id (empty for environment-level roles).
		orgID := conv.Ternary(orgEvent.Object == "organization", orgEvent.ID, orgEvent.OrganizationID)
		if orgID != "" {
			eventLogger = eventLogger.With(attr.SlogWorkOSEventOrganizationID(orgID))
		}

		eventID, err := p.handleEvent(ctx, eventLogger, workosOrgID, event)
		if err != nil {
			return lastEventID, oops.E(oops.CodeUnexpected, err, "failed to handle WorkOS event").Log(ctx, eventLogger)
		}
		if eventID != "" {
			lastEventID = eventID
		}
	}

	return lastEventID, nil
}

// handleEvent will be implemented in a subsequent PR.
//
// Note: the cursor advances as soon as this returns nil, even though the
// dispatched handler is currently a no-op. If the workflow runs before the
// real handlers land, all in-flight WorkOS events will be consumed and the
// cursor will move past them — real handlers will only see events going
// forward. That is the intended design (sync handles forward, not history;
// historical state is reconciled by the future backfill workflow).
func (p *ProcessWorkOSOrganizationEvents) handleEvent(ctx context.Context, logger *slog.Logger, workosOrgID string, event events.Event) (string, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := handleOrganizationEvent(ctx, logger, dbtx, event); err != nil {
		return "", err
	}

	if _, err := workosrepo.New(dbtx).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{
		WorkosOrganizationID: workosOrgID,
		LastEventID:          event.ID,
	}); err != nil {
		return "", fmt.Errorf("set organization sync last event ID: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	return event.ID, nil
}

// handleOrganizationEvent dispatches an organization.* or organization_role.*
// WorkOS event to its handler. Each handler is responsible for the
// ShouldProcessEvent guard against duplicate apply.
func handleOrganizationEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	switch event.Event {
	case string(workos.EventKindOrganizationCreated), string(workos.EventKindOrganizationUpdated):
		return handleOrganizationUpsert(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationDeleted):
		return handleOrganizationDeleted(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationRoleCreated), string(workos.EventKindOrganizationRoleUpdated):
		return handleRoleUpsert(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationRoleDeleted):
		return handleRoleDeleted(ctx, logger, dbtx, event)
	}

	return oops.Permanent(fmt.Errorf("unhandled workos organization event type: %s", event.Event))
}

// workosOrganizationEventPayload is the relevant subset of an
// organization.{created,updated,deleted} event payload.
type workosOrganizationEventPayload struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ExternalID string    `json:"external_id"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// handleOrganizationUpsert applies an organization.created or
// organization.updated event. Maps the WorkOS organization to a Gram org by
// looking up workos_id; falls back to the payload's external_id for orgs not
// yet linked.
func handleOrganizationUpsert(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosOrganizationEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return oops.Permanent(fmt.Errorf("unmarshal organization event payload: %w", err))
	}

	repo := orgrepo.New(dbtx)

	row, err := repo.GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.ID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if payload.ExternalID == "" {
			return oops.Permanent(fmt.Errorf("workos organization %q has no external_id and no existing mapping", payload.ID))
		}
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.ID, err)
	}

	var lastEventID *string
	if row.WorkosLastEventID.Valid {
		lastEventID = &row.WorkosLastEventID.String
	}
	var rowUpdatedAt *time.Time
	if row.WorkosUpdatedAt.Valid {
		rowUpdatedAt = &row.WorkosUpdatedAt.Time
	}
	if !ShouldProcessEvent(lastEventID, rowUpdatedAt, event.ID, payload.UpdatedAt) {
		return nil
	}

	organizationID := payload.ExternalID
	if err == nil {
		organizationID = row.ID
	}

	_, err = repo.UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:                organizationID,
		Name:              payload.Name,
		Slug:              conv.ToSlug(payload.Name),
		WorkosID:          conv.ToPGText(payload.ID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(payload.UpdatedAt),
		WorkosLastEventID: conv.ToPGText(event.ID),
	})
	if err != nil {
		return fmt.Errorf("upsert organization %q from workos event: %w", payload.ID, err)
	}

	return nil
}

func handleOrganizationDeleted(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosOrganizationEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return oops.Permanent(fmt.Errorf("unmarshal organization delete event payload: %w", err))
	}

	repo := orgrepo.New(dbtx)
	row, err := repo.GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.ID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "skipping organization delete for unknown org", attr.SlogWorkOSOrganizationID(payload.ID))
		return nil
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.ID, err)
	}

	var lastEventID *string
	if row.WorkosLastEventID.Valid {
		lastEventID = &row.WorkosLastEventID.String
	}
	var rowUpdatedAt *time.Time
	if row.WorkosUpdatedAt.Valid {
		rowUpdatedAt = &row.WorkosUpdatedAt.Time
	}
	if !ShouldProcessEvent(lastEventID, rowUpdatedAt, event.ID, payload.UpdatedAt) {
		return nil
	}

	if _, err := repo.DisableOrganizationByWorkosID(ctx, orgrepo.DisableOrganizationByWorkosIDParams{
		WorkosID:          conv.ToPGText(payload.ID),
		WorkosLastEventID: conv.ToPGText(event.ID),
	}); err != nil {
		return fmt.Errorf("disable organization %q: %w", payload.ID, err)
	}

	return nil
}

// workosRoleEventPayload is the relevant subset of an organization_role.*
// event payload. OrganizationID is empty for environment-level roles.
type workosRoleEventPayload struct {
	OrganizationID string     `json:"organization_id"`
	Slug           string     `json:"slug"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Type           string     `json:"type"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at"`
}

// handleRoleUpsert applies an organization_role.created or
// organization_role.updated event. Routes EnvironmentRole types to
// global_roles and everything else to organization_roles.
func handleRoleUpsert(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosRoleEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return oops.Permanent(fmt.Errorf("unmarshal role event payload: %w", err))
	}

	if payload.Type == workosRoleTypeEnvironment {
		return upsertGlobalRole(ctx, dbtx, event, payload)
	}

	return upsertOrganizationRole(ctx, logger, dbtx, event, payload)
}

func upsertGlobalRole(ctx context.Context, dbtx database.DBTX, event events.Event, payload workosRoleEventPayload) error {
	repo := accessrepo.New(dbtx)

	existing, err := repo.GetGlobalRoleBySlug(ctx, payload.Slug)
	// if the error is due to no rows found, we're ok to proceed (new role)
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

func upsertOrganizationRole(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event, payload workosRoleEventPayload) error {
	org, err := orgrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "skipping role event for unknown organization", attr.SlogWorkOSOrganizationID(payload.OrganizationID))
		return nil
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.OrganizationID, err)
	}

	repo := accessrepo.New(dbtx)
	existing, err := repo.GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: org.ID,
		WorkosSlug:     payload.Slug,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get organization role %q: %w", payload.Slug, err)
	}

	var lastEventID *string
	if existing.WorkosLastEventID.Valid {
		lastEventID = &existing.WorkosLastEventID.String
	}
	rowUpdatedAt := existing.WorkosUpdatedAt.Time
	if !ShouldProcessEvent(lastEventID, &rowUpdatedAt, event.ID, payload.UpdatedAt) {
		return nil
	}

	if err := repo.UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    org.ID,
		WorkosSlug:        payload.Slug,
		WorkosName:        payload.Name,
		WorkosDescription: conv.ToPGTextEmpty(payload.Description),
		WorkosCreatedAt:   conv.ToPGTimestamptz(payload.CreatedAt),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(payload.UpdatedAt),
		WorkosLastEventID: conv.ToPGText(event.ID),
	}); err != nil {
		return fmt.Errorf("upsert organization role %q: %w", payload.Slug, err)
	}

	return nil
}

func handleRoleDeleted(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosRoleEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return oops.Permanent(fmt.Errorf("unmarshal role delete event payload: %w", err))
	}

	deletedAt := payload.DeletedAt
	if deletedAt == nil || deletedAt.IsZero() {
		deletedAt = &event.CreatedAt
	}

	if payload.Type == workosRoleTypeEnvironment {
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

	org, err := orgrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "skipping role delete for unknown organization", attr.SlogWorkOSOrganizationID(payload.OrganizationID))
		return nil
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.OrganizationID, err)
	}

	repo := accessrepo.New(dbtx)
	existing, err := repo.GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: org.ID,
		WorkosSlug:     payload.Slug,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("get organization role %q: %w", payload.Slug, err)
	}

	var lastEventID *string
	if existing.WorkosLastEventID.Valid {
		lastEventID = &existing.WorkosLastEventID.String
	}
	rowUpdatedAt := existing.WorkosUpdatedAt.Time
	if !ShouldProcessEvent(lastEventID, &rowUpdatedAt, event.ID, payload.UpdatedAt) {
		return nil
	}

	if _, err := repo.MarkOrganizationRoleDeleted(ctx, accessrepo.MarkOrganizationRoleDeletedParams{
		OrganizationID:    org.ID,
		WorkosSlug:        payload.Slug,
		WorkosDeletedAt:   conv.ToPGTimestamptz(*deletedAt),
		WorkosLastEventID: conv.ToPGText(event.ID),
	}); err != nil {
		return fmt.Errorf("mark organization role %q deleted: %w", payload.Slug, err)
	}

	if _, err := repo.DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: org.ID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, payload.Slug),
	}); err != nil {
		return fmt.Errorf("delete grants for role %q: %w", payload.Slug, err)
	}

	return nil
}

// ShouldProcessEvent decides whether a WorkOS event should be applied to a
// row, guarding against duplicate-apply when the sync replays history (e.g.
// reconcile schedule overlapping with webhook delivery, or a manual
// backfill).
//
// Algorithm:
//
//   - If the row has no recorded last_event_id, it has not yet been touched
//     by an event-driven update. Use the row's workos_updated_at as the
//     baseline: apply the event only if its payload's updated_at is at least
//     as recent as the row.
//   - Otherwise, compare event IDs lexicographically. WorkOS event IDs are
//     time-ordered (ULIDs), so a strictly greater ID means the event is
//     newer than the last one we applied.
//
// Inputs are nilable to model NULL columns directly.
func ShouldProcessEvent(rowLastEventID *string, rowWorkOSUpdatedAt *time.Time, eventID string, eventUpdatedAt time.Time) bool {
	if rowLastEventID == nil || *rowLastEventID == "" {
		if rowWorkOSUpdatedAt == nil {
			return true
		}
		return !eventUpdatedAt.Before(*rowWorkOSUpdatedAt)
	}
	return eventID > *rowLastEventID
}
