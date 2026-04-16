package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	"github.com/workos/workos-go/v6/pkg/events"
)

type ProcessWorkOSOrganizationEventsParams struct {
	WorkOSOrganizationID string  `json:"workos_organization_id,omitempty"`
	SinceEventID         *string `json:"since_event_id,omitempty"`
}

type ProcessWorkOSOrganizationEventsResult struct {
	SinceEventID string `json:"since_event_id"`
	LastEventID  string `json:"last_event_id"`
}

type ProcessWorkOSOrganizationEvents struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	eventsClient *events.Client
}

func NewProcessWorkOSOrganizationEvents(logger *slog.Logger, db *pgxpool.Pool, eventsClient *events.Client) *ProcessWorkOSOrganizationEvents {
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
	// get the latest cursor from the database
	sinceEventID := "todo_cursor"

	options := events.ListEventsOpts{
		Events: stringifyEventKinds(
			workos.EventKindOrganizationCreated,
			workos.EventKindOrganizationDeleted,
			workos.EventKindOrganizationUpdated,

			workos.EventKindOrganizationMembershipCreated,
			workos.EventKindOrganizationMembershipDeleted,
			workos.EventKindOrganizationMembershipUpdated,

			workos.EventKindOrganizationRoleCreated,
			workos.EventKindOrganizationRoleDeleted,
			workos.EventKindOrganizationRoleUpdated,
		),
		Limit:          100,
		After:          sinceEventID,
		RangeStart:     "",
		RangeEnd:       "",
		OrganizationId: workOSOrgID,
	}

	lastEventID := sinceEventID

	for page, err := range slurpEvents(ctx, p.eventsClient, options) {
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list WorkOS events").Log(ctx, logger)
		}

		lastEventID, err = p.handlePage(ctx, logger, page)
		if err != nil {
			return nil, err
		}
	}

	return &ProcessWorkOSOrganizationEventsResult{
		SinceEventID: sinceEventID,
		LastEventID:  lastEventID,
	}, nil
}

func (p *ProcessWorkOSOrganizationEvents) handlePage(ctx context.Context, logger *slog.Logger, page []events.Event) (string, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to begin database transaction for workos event processing").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	wrepo := workosrepo.New(dbtx)

	var lastEventID string
	var eventErr error
	for _, event := range page {
		logger = logger.With(
			attr.SlogWorkOSEventID(event.ID),
			attr.SlogWorkOSEventType(event.Event),
		)
		var orgEvent workosOrgEvent
		if err := json.Unmarshal(event.Data, &orgEvent); err != nil {
			eventErr = oops.E(oops.CodeUnexpected, err, "failed to unmarshal workos organization event data").Log(ctx, logger)
			break
		}

		orgID := conv.Ternary(orgEvent.Object == "organization", orgEvent.ID, orgEvent.OrganizationID)
		if orgID != "" {
			return "", oops.E(oops.CodeUnexpected, nil, "unexpected non-organization event object: %s", orgEvent.Object).Log(ctx, logger)
		}

		logger = logger.With(
			attr.SlogWorkOSEventOrganizationID(orgEvent.OrganizationID),
		)

		err := workos.HandleOrganizationEvent(ctx, logger, dbtx, event)
		if err != nil {
			eventErr = oops.E(oops.CodeUnexpected, err, "failed to handle WorkOS event").Log(ctx, logger)
			break
		}
		if _, err := wrepo.SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{
			WorkosOrganizationID: "",
			LastEventID:          event.ID,
		}); err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "failed to set organization sync last event ID").Log(ctx, logger)
		}
		lastEventID = event.ID
	}

	if err := dbtx.Commit(ctx); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to commit database transaction for workos event processing").Log(ctx, logger)
	}

	if eventErr != nil {
		return "", eventErr
	}

	return lastEventID, nil
}

func slurpEvents(ctx context.Context, client *events.Client, opts events.ListEventsOpts) iter.Seq2[[]events.Event, error] {
	return func(yield func([]events.Event, error) bool) {
		loadMore := true
		cursor := opts.After
		for loadMore {
			opts.After = cursor
			resp, err := client.ListEvents(ctx, opts)
			if err != nil {
				yield(nil, fmt.Errorf("fetch page: %w", err))
				return
			}

			if !yield(resp.Data, nil) {
				return
			}

			loadMore = resp.ListMetadata.After != ""
			cursor = resp.ListMetadata.After
		}
	}
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
