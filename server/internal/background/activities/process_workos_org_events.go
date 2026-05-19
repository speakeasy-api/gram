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
	"github.com/speakeasy-api/gram/server/internal/auth/orgslug"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const workosOrgEventsPageSize = 100

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
// since the stored cursor, applying supported organization, role, and
// membership events in a transaction before advancing the cursor.
type ProcessWorkOSOrganizationEvents struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	workosClient WorkOSClient
}

func NewProcessWorkOSOrganizationEvents(logger *slog.Logger, db *pgxpool.Pool, workosClient WorkOSClient) *ProcessWorkOSOrganizationEvents {
	return &ProcessWorkOSOrganizationEvents{
		db:           db,
		logger:       logger,
		workosClient: workosClient,
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

	resp, err := p.workosClient.ListEvents(ctx, events.ListEventsOpts{
		Events: []string{
			string(workos.EventKindOrganizationCreated),
			string(workos.EventKindOrganizationUpdated),
			string(workos.EventKindOrganizationDeleted),

			string(workos.EventKindOrganizationRoleCreated),
			string(workos.EventKindOrganizationRoleDeleted),
			string(workos.EventKindOrganizationRoleUpdated),

			string(workos.EventKindOrganizationMembershipCreated),
			string(workos.EventKindOrganizationMembershipUpdated),
			string(workos.EventKindOrganizationMembershipDeleted),
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
		lastEventID = eventID
	}

	return lastEventID, nil
}

// handleEvent applies a single WorkOS event and advances the per-organization
// cursor in the same transaction.
func (p *ProcessWorkOSOrganizationEvents) handleEvent(ctx context.Context, logger *slog.Logger, workosOrgID string, event events.Event) (string, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	externalIDUpdate, err := handleOrganizationEvent(ctx, logger, dbtx, event)
	if err != nil {
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

	if externalIDUpdate != nil {
		if err := p.workosClient.EnsureOrgExternalID(ctx, externalIDUpdate.workosOrgID, externalIDUpdate.externalID); err != nil {
			logger.WarnContext(ctx, "failed to set WorkOS organization external ID", attr.SlogError(err))
		}
	}

	return event.ID, nil
}

// handleOrganizationEvent dispatches a WorkOS event scoped to a specific
// organization to its handler. Each handler is responsible for the
// ShouldProcessEvent guard against duplicate apply.
func handleOrganizationEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) (*workosOrgExternalIDUpdate, error) {
	switch event.Event {
	case string(workos.EventKindOrganizationCreated), string(workos.EventKindOrganizationUpdated):
		return handleOrganizationUpsert(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationDeleted):
		return nil, handleOrganizationDeleted(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationRoleCreated), string(workos.EventKindOrganizationRoleUpdated):
		return nil, handleRoleUpsert(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationRoleDeleted):
		return nil, handleRoleDeleted(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationMembershipCreated), string(workos.EventKindOrganizationMembershipUpdated):
		return nil, handleOrganizationMembershipUpsert(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationMembershipDeleted):
		return nil, handleOrganizationMembershipDeleted(ctx, logger, dbtx, event)
	}

	return nil, oops.Permanent(fmt.Errorf("unhandled workos organization event type: %s", event.Event))
}

// workosOrganizationEventPayload is the relevant subset of an
// organization.{created,updated,deleted} event payload.
type workosOrganizationEventPayload struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ExternalID string    `json:"external_id"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type workosOrgExternalIDUpdate struct {
	workosOrgID string
	externalID  string
}

// handleOrganizationUpsert applies an organization.created or
// organization.updated event. WorkOS external_id is the Gram organization ID
// when present. If WorkOS has no external_id, Gram derives a deterministic org
// ID from the WorkOS org ID and writes that ID back to WorkOS after committing
// the local transaction. WorkOS owns name/workos_id metadata, but never updates
// an existing Gram slug.
func handleOrganizationUpsert(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) (*workosOrgExternalIDUpdate, error) {
	var payload workosOrganizationEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return nil, oops.Permanent(fmt.Errorf("unmarshal organization event payload: %w", err))
	}

	repo := orgrepo.New(dbtx)

	row, err := repo.GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.ID))
	switch {
	case err == nil:
		if err := updateOrganizationFromWorkOSEvent(ctx, repo, row, payload, event.ID); err != nil {
			return nil, err
		}
		if payload.ExternalID == "" {
			return &workosOrgExternalIDUpdate{workosOrgID: payload.ID, externalID: row.ID}, nil
		}
		return nil, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Resolve below by external_id or by a deterministic ID derived from
		// the WorkOS org ID.
	case err != nil:
		return nil, fmt.Errorf("get organization for workos id %q: %w", payload.ID, err)
	}

	organizationID := payload.ExternalID
	needsExternalIDUpdate := false
	if organizationID == "" {
		organizationID = orgid.FromWorkOSID(payload.ID)
		needsExternalIDUpdate = true
	}

	row, err = repo.GetOrganizationMetadata(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := createOrganizationFromWorkOSEvent(ctx, repo, payload, event.ID, organizationID); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, fmt.Errorf("get organization metadata %q: %w", organizationID, err)
	default:
		if err := updateOrganizationFromWorkOSEvent(ctx, repo, row, payload, event.ID); err != nil {
			return nil, err
		}
	}

	if needsExternalIDUpdate {
		return &workosOrgExternalIDUpdate{workosOrgID: payload.ID, externalID: organizationID}, nil
	}
	return nil, nil
}

func createOrganizationFromWorkOSEvent(ctx context.Context, repo *orgrepo.Queries, payload workosOrganizationEventPayload, eventID string, organizationID string) error {
	slug := orgslug.Slugify(payload.Name)
	if slug == "" {
		return fmt.Errorf("slugify workos organization name %q: empty slug", payload.Name)
	}
	if err := repo.LockOrganizationSlug(ctx, slug); err != nil {
		return fmt.Errorf("lock organization slug %q: %w", slug, err)
	}
	uniqueSlug, err := orgslug.FindUnique(ctx, repo, slug)
	if err != nil {
		return fmt.Errorf("find unique slug for workos organization %q: %w", payload.ID, err)
	}

	row, err := repo.GetOrganizationMetadata(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return fmt.Errorf("get organization metadata %q: %w", organizationID, err)
	default:
		return updateOrganizationFromWorkOSEvent(ctx, repo, row, payload, eventID)
	}

	_, err = repo.UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:                organizationID,
		Name:              payload.Name,
		Slug:              uniqueSlug,
		WorkosID:          conv.ToPGText(payload.ID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(payload.UpdatedAt),
		WorkosLastEventID: conv.ToPGText(eventID),
	})
	if err != nil {
		return fmt.Errorf("upsert organization %q from workos event: %w", payload.ID, err)
	}
	return nil
}

func updateOrganizationFromWorkOSEvent(ctx context.Context, repo *orgrepo.Queries, row orgrepo.OrganizationMetadatum, payload workosOrganizationEventPayload, eventID string) error {
	// If external_id points at a Gram row linked to another WorkOS org, this is
	// a deliberate relink. Its stored cursor belongs to the prior WorkOS org.
	useExistingCursor := !row.WorkosID.Valid || row.WorkosID.String == "" || row.WorkosID.String == payload.ID
	var lastEventID *string
	if row.WorkosLastEventID.Valid && useExistingCursor {
		lastEventID = &row.WorkosLastEventID.String
	}
	var rowUpdatedAt *time.Time
	if row.WorkosUpdatedAt.Valid && useExistingCursor {
		rowUpdatedAt = &row.WorkosUpdatedAt.Time
	}
	if !ShouldProcessEvent(lastEventID, rowUpdatedAt, eventID, payload.UpdatedAt) {
		return nil
	}

	_, err := repo.UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID:                row.ID,
		Name:              payload.Name,
		WorkosID:          conv.ToPGText(payload.ID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(payload.UpdatedAt),
		WorkosLastEventID: conv.ToPGText(eventID),
	})
	if err != nil {
		return fmt.Errorf("update organization %q from workos event: %w", payload.ID, err)
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
// organization_role.updated event.
func handleRoleUpsert(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosRoleEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return oops.Permanent(fmt.Errorf("unmarshal role event payload: %w", err))
	}

	return upsertOrganizationRole(ctx, logger, dbtx, event, payload)
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
