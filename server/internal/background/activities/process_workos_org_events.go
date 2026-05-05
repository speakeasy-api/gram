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

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

const workosOrgEventsPageSize = 100

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

		orgID := conv.Ternary(orgEvent.Object == "organization", orgEvent.ID, orgEvent.OrganizationID)
		if orgID == "" {
			return lastEventID, oops.E(oops.CodeUnexpected, nil, "unexpected non-organization event object: %s", orgEvent.Object).Log(ctx, eventLogger)
		}

		eventLogger = eventLogger.With(attr.SlogWorkOSEventOrganizationID(orgID))

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

// handleOrganizationEvent applies an organization.* WorkOS event to local
// state. The implementation lands in a follow-up PR — see [handleEvent] for
// the consequence of leaving this as a no-op while the workflow is live.
func handleOrganizationEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	// TODO: implement this method
	return nil
}
