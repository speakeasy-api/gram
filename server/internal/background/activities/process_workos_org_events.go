package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
	WorkOSOrganizationID string  `json:"workos_organization_id,omitempty"`
	SinceEventID         *string `json:"since_event_id,omitempty"`
}

type ProcessWorkOSOrganizationEventsResult struct {
	SinceEventID string `json:"since_event_id"`
	LastEventID  string `json:"last_event_id"`
	HasMore      bool   `json:"has_more"`
}

// ProcessWorkOSOrganizationEvents pages through WorkOS organization-scoped events
// since the stored cursor, advancing the cursor as it goes. This PR introduces
// the plumbing only — actual event handling (upserting org metadata, role rows,
// etc.) is wired in a follow-up. For now the activity advances the cursor and
// returns whether more pages remain.
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
		},
		Limit:          workosOrgEventsPageSize,
		After:          sinceEventID,
		OrganizationId: workOSOrgID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list WorkOS events").Log(ctx, logger)
	}

	lastEventID, err := p.advanceCursor(ctx, logger, workOSOrgID, resp.Data)
	if err != nil {
		return nil, err
	}

	return &ProcessWorkOSOrganizationEventsResult{
		SinceEventID: sinceEventID,
		LastEventID:  lastEventID,
		HasMore:      len(resp.Data) == workosOrgEventsPageSize,
	}, nil
}

// advanceCursor walks the page in order, persisting the last event ID after each
// event in its own transaction. Event handling (the actual side effects) is
// added in a follow-up PR — this loop currently just records progress so the
// next page picks up where we left off.
func (p *ProcessWorkOSOrganizationEvents) advanceCursor(ctx context.Context, logger *slog.Logger, workosOrgID string, page []events.Event) (string, error) {
	var lastEventID string
	for _, event := range page {
		if err := p.persistCursor(ctx, workosOrgID, event.ID); err != nil {
			return lastEventID, oops.E(oops.CodeUnexpected, err, "failed to advance WorkOS org sync cursor").Log(ctx, logger)
		}
		lastEventID = event.ID
	}
	return lastEventID, nil
}

func (p *ProcessWorkOSOrganizationEvents) persistCursor(ctx context.Context, workosOrgID, eventID string) error {
	if _, err := workosrepo.New(p.db).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{
		WorkosOrganizationID: workosOrgID,
		LastEventID:          eventID,
	}); err != nil {
		return fmt.Errorf("set organization sync last event ID: %w", err)
	}
	return nil
}
