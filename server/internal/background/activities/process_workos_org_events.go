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
	"github.com/workos/workos-go/v6/pkg/events"
)

type ProcessWorkOSOrganizationEventsParams struct {
	WorkOSOrganizationID string  `json:"workos_organization_id,omitempty"`
	SinceEventID         *string `json:"since_event_id,omitempty"`
}

type ProcessWorkOSOrganizationEventsResult struct {
	SinceEventID string `json:"since_event_id"`
	LastEventID  string `json:"last_event_id"`
	HasMore      bool   `json:"has_more"`
}

// EventsLister is the subset of events.Client used by this activity.
type EventsLister interface {
	ListEvents(ctx context.Context, opts events.ListEventsOpts) (events.ListEventsResponse, error)
}

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
	// handle errors
	return p.do(ctx, params)
}

func (p *ProcessWorkOSOrganizationEvents) do(ctx context.Context, params ProcessWorkOSOrganizationEventsParams) (*ProcessWorkOSOrganizationEventsResult, error) {
	workOSOrgID := params.WorkOSOrganizationID

	logger := p.logger

	sinceEventID := conv.PtrValOr(params.SinceEventID, "")
	if sinceEventID == "" {
		cursor, err := workosrepo.New(p.db).GetOrganizationSyncLastEventID(ctx, workOSOrgID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// no cursor yet, full sync from beginning
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get organization sync last event ID").Log(ctx, logger)
		default:
			sinceEventID = cursor
		}
	}

	pageSize := 100

	options := events.ListEventsOpts{
		Events: stringifyEventKinds(
			workos.EventKindOrganizationCreated,
			workos.EventKindOrganizationDeleted,
			workos.EventKindOrganizationUpdated,

			workos.EventKindOrganizationRoleCreated,
			workos.EventKindOrganizationRoleDeleted,
			workos.EventKindOrganizationRoleUpdated,
		),
		Limit:          pageSize,
		After:          sinceEventID,
		RangeStart:     "",
		RangeEnd:       "",
		OrganizationId: workOSOrgID,
	}

	resp, err := p.eventsClient.ListEvents(ctx, options)
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
		HasMore:      len(resp.Data) == pageSize,
	}, nil
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

		orgID := conv.Ternary(orgEvent.Object == "organization", orgEvent.ID, orgEvent.OrganizationID)
		if orgID == "" {
			return lastEventID, oops.E(oops.CodeUnexpected, nil, "unexpected non-organization event object: %s", orgEvent.Object).Log(ctx, eventLogger)
		}

		eventLogger = eventLogger.With(attr.SlogWorkOSEventOrganizationID(orgEvent.OrganizationID))

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

func stringifyEventKinds(eventKinds ...workos.EventKind) []string {
	strs := make([]string, len(eventKinds))
	for i, ek := range eventKinds {
		strs[i] = string(ek)
	}
	return strs
}

type workosOrgEvent struct {
	ID             string `json:"id"`
	Object         string `json:"object"`
	OrganizationID string `json:"organization_id"`
}

func handleOrganizationEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	switch event.Event {
	case string(workos.EventKindOrganizationCreated):
		return handleOrganizationCreatedOrUpdated(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationDeleted):
		return handleOrganizationDeleted(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationUpdated):
		return handleOrganizationCreatedOrUpdated(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationRoleCreated):
		return handleOrganizationRoleUpsert(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationRoleDeleted):
		return handleOrganizationRoleDeleted(ctx, logger, dbtx, event)
	case string(workos.EventKindOrganizationRoleUpdated):
		return handleOrganizationRoleUpsert(ctx, logger, dbtx, event)
	}

	return oops.Permanent(fmt.Errorf("unhandled workos organization event: %s", event.Event))
}

type workosOrganizationEvent struct {
	ID         string `json:"id"`
	Object     string `json:"object"`
	Name       string `json:"name"`
	ExternalID string `json:"external_id"`
}

// handleOrganizationCreatedOrUpdated will create or update an organization internally.
// It attempts to fetch an organization ID mapped to a workOS ID. If it cannot be found, it relies on
// the external_id from WorkOS (which should exist and map to an organization in speakeasy registry).
// It then creates or upserts the metadata for that organization.
func handleOrganizationCreatedOrUpdated(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosOrganizationEvent
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal organization event: %w", err)
	}

	repo := orgrepo.New(dbtx)

	organizationID, err := repo.GetOrganizationIDByWorkosID(ctx, conv.ToPGText(payload.ID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// when an organization is not yet mapped to workOS interna
		if payload.ExternalID == "" {
			return fmt.Errorf("workos organization %q has no external_id", payload.ID)
		}
		organizationID = payload.ExternalID

	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.ID, err)
	}

	_, err = repo.UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:       organizationID,
		Name:     payload.Name,
		Slug:     conv.ToSlug(payload.Name),
		WorkosID: conv.ToPGText(payload.ID),
	})
	if err != nil {
		return fmt.Errorf("upsert organization %q from workos event: %w", payload.ID, err)
	}

	return nil
}

func handleOrganizationDeleted(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosOrganizationEvent
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal organization delete event: %w", err)
	}

	rows, err := orgrepo.New(dbtx).DisableOrganizationByWorkosID(ctx, conv.ToPGText(payload.ID))
	if err != nil {
		return fmt.Errorf("disable organization %q: %w", payload.ID, err)
	}
	if rows == 0 {
		logger.DebugContext(ctx, "skipping organization delete for unknown org", attr.SlogWorkOSOrganizationID(payload.ID))
	}

	return nil
}

const workosRoleTypeEnvironment = "EnvironmentRole"

type workosRoleEvent struct {
	ID             string     `json:"id"`
	Object         string     `json:"object"`
	OrganizationID string     `json:"organization_id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	Type           string     `json:"type"` // "EnvironmentRole" or "OrganizationRole"
	Description    string     `json:"description"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at"`
}

func handleOrganizationRoleUpsert(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosRoleEvent
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal organization role event: %w", err)
	}

	if payload.Type == workosRoleTypeEnvironment {
		err := accessrepo.New(dbtx).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
			WorkosSlug:        payload.Slug,
			WorkosName:        payload.Name,
			WorkosDescription: conv.ToPGTextEmpty(payload.Description),
			WorkosCreatedAt:   conv.ToPGTimestamptzEmpty(payload.CreatedAt),
			WorkosUpdatedAt:   conv.ToPGTimestamptzEmpty(payload.UpdatedAt),
			WorkosLastEventID: conv.ToPGText(event.ID),
		})
		if err != nil {
			return fmt.Errorf("upsert global role %q: %w", payload.Slug, err)
		}
		return nil
	}

	organizationID, err := orgrepo.New(dbtx).GetOrganizationIDByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "skipping role event for unknown organization", attr.SlogWorkOSOrganizationID(payload.OrganizationID))
		return nil
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.OrganizationID, err)
	}

	err = accessrepo.New(dbtx).UpsertRole(ctx, accessrepo.UpsertRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        payload.Slug,
		WorkosName:        payload.Name,
		WorkosDescription: conv.ToPGTextEmpty(payload.Description),
		WorkosCreatedAt:   conv.ToPGTimestamptzEmpty(payload.CreatedAt),
		WorkosUpdatedAt:   conv.ToPGTimestamptzEmpty(payload.UpdatedAt),
		WorkosLastEventID: conv.ToPGText(event.ID),
	})
	if err != nil {
		return fmt.Errorf("upsert organization role %q: %w", payload.Slug, err)
	}

	return nil
}

func handleOrganizationRoleDeleted(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosRoleEvent
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal organization role delete event: %w", err)
	}

	deletedAt := payload.DeletedAt
	if deletedAt == nil {
		deletedAt = &event.CreatedAt
	}
	if deletedAt.IsZero() {
		now := time.Now().UTC()
		deletedAt = &now
	}

	if payload.Type == workosRoleTypeEnvironment {
		_, err := accessrepo.New(dbtx).MarkGlobalRoleDeleted(ctx, accessrepo.MarkGlobalRoleDeletedParams{
			WorkosDeletedAt:   conv.ToPGTimestamptzEmpty(*deletedAt),
			WorkosLastEventID: conv.ToPGText(event.ID),
			WorkosSlug:        payload.Slug,
		})
		if err != nil {
			return fmt.Errorf("mark global role deleted %q: %w", payload.Slug, err)
		}
		return nil
	}

	organizationID, err := orgrepo.New(dbtx).GetOrganizationIDByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.DebugContext(ctx, "skipping role delete for unknown organization", attr.SlogWorkOSOrganizationID(payload.OrganizationID))
		return nil
	case err != nil:
		return fmt.Errorf("get organization by workos id %q: %w", payload.OrganizationID, err)
	}

	_, err = accessrepo.New(dbtx).MarkRoleDeleted(ctx, accessrepo.MarkRoleDeletedParams{
		WorkosDeletedAt:   conv.ToPGTimestamptzEmpty(*deletedAt),
		WorkosLastEventID: conv.ToPGText(event.ID),
		OrganizationID:    organizationID,
		WorkosSlug:        payload.Slug,
	})
	if err != nil {
		return fmt.Errorf("mark role deleted %q: %w", payload.Slug, err)
	}

	_, err = accessrepo.New(dbtx).DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: organizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, payload.Slug),
	})
	if err != nil {
		return fmt.Errorf("delete grants for role %q: %w", payload.Slug, err)
	}

	return nil
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
