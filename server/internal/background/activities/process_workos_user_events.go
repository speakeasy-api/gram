package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	"github.com/speakeasy-api/gram/server/internal/users"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const workosUserEventsPageSize = 100

type ProcessWorkOSUserEventsParams struct {
	WorkOSUserID string  `json:"workos_user_id"`
	SinceEventID *string `json:"since_event_id,omitempty"`
}

type ProcessWorkOSUserEventsResult struct {
	SinceEventID string `json:"since_event_id"`
	LastEventID  string `json:"last_event_id"`
	HasMore      bool   `json:"has_more"`
}

type ProcessWorkOSUserEvents struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	workosClient WorkOSClient
}

func NewProcessWorkOSUserEvents(logger *slog.Logger, db *pgxpool.Pool, workosClient WorkOSClient) *ProcessWorkOSUserEvents {
	return &ProcessWorkOSUserEvents{
		db:           db,
		logger:       logger,
		workosClient: workosClient,
	}
}

func (p *ProcessWorkOSUserEvents) Do(ctx context.Context, params ProcessWorkOSUserEventsParams) (*ProcessWorkOSUserEventsResult, error) {
	logger := p.logger.With(attr.SlogWorkOSUserID(params.WorkOSUserID))
	if params.WorkOSUserID == "" {
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("missing WorkOS user ID"), "missing WorkOS user ID").Log(ctx, logger)
	}

	sinceEventID := conv.PtrValOr(params.SinceEventID, "")
	if sinceEventID == "" {
		cursor, err := workosrepo.New(p.db).GetUserSyncLastEventID(ctx, conv.ToPGText(params.WorkOSUserID))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "get user sync cursor").Log(ctx, logger)
		default:
			sinceEventID = cursor
		}
	}

	resp, err := p.workosClient.ListEvents(ctx, events.ListEventsOpts{
		Events: []string{
			string(workos.EventKindUserCreated),
			string(workos.EventKindUserUpdated),
			string(workos.EventKindUserDeleted),
		},
		Limit:          workosUserEventsPageSize,
		After:          sinceEventID,
		OrganizationId: "",
		RangeStart:     "",
		RangeEnd:       "",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list WorkOS user events").Log(ctx, logger)
	}

	lastEventID, err := p.handlePage(ctx, logger, params.WorkOSUserID, resp.Data)
	if err != nil {
		return nil, err
	}

	return &ProcessWorkOSUserEventsResult{
		SinceEventID: sinceEventID,
		LastEventID:  lastEventID,
		HasMore:      len(resp.Data) == workosUserEventsPageSize,
	}, nil
}

func (p *ProcessWorkOSUserEvents) handlePage(ctx context.Context, logger *slog.Logger, workosUserID string, page []events.Event) (string, error) {
	var lastEventID string
	for _, event := range page {
		eventLogger := logger.With(
			attr.SlogWorkOSEventID(event.ID),
			attr.SlogWorkOSEventType(event.Event),
		)

		eventID, err := p.handleEvent(ctx, eventLogger, workosUserID, event)
		if err != nil {
			return lastEventID, oops.E(oops.CodeUnexpected, err, "handle WorkOS user event").Log(ctx, eventLogger)
		}
		if eventID != "" {
			lastEventID = eventID
		}
	}
	return lastEventID, nil
}

func (p *ProcessWorkOSUserEvents) handleEvent(ctx context.Context, logger *slog.Logger, workosUserID string, event events.Event) (string, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	externalIDUpdate, err := p.handleUserEvent(ctx, dbtx, workosUserID, event)
	if err != nil {
		return "", err
	}

	if _, err := workosrepo.New(dbtx).SetUserSyncLastEventID(ctx, workosrepo.SetUserSyncLastEventIDParams{
		WorkosUserID: conv.ToPGText(workosUserID),
		LastEventID:  event.ID,
	}); err != nil {
		return "", fmt.Errorf("set user sync cursor: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	if externalIDUpdate != nil {
		if err := p.workosClient.UpdateUserExternalID(ctx, externalIDUpdate.workosUserID, externalIDUpdate.externalID); err != nil {
			logger.WarnContext(ctx, "failed to set WorkOS user external ID", attr.SlogError(err))
		}
	}

	return event.ID, nil
}

type workosUserExternalIDUpdate struct {
	workosUserID string
	externalID   string
}

type workosUserEventPayload struct {
	ID                string    `json:"id"`
	ExternalID        string    `json:"external_id"`
	Email             string    `json:"email"`
	FirstName         string    `json:"first_name"`
	LastName          string    `json:"last_name"`
	ProfilePictureURL string    `json:"profile_picture_url"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	DeletedAt         time.Time `json:"deleted_at"`
}

func (p *ProcessWorkOSUserEvents) handleUserEvent(ctx context.Context, dbtx database.DBTX, workosUserID string, event events.Event) (*workosUserExternalIDUpdate, error) {
	var payload workosUserEventPayload

	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return nil, oops.Permanent(fmt.Errorf("unmarshal user event payload: %w", err))
	}
	if payload.ID == "" {
		return nil, oops.Permanent(fmt.Errorf("invalid user event payload missing ID"))
	}

	if payload.ID != workosUserID {
		return nil, nil
	}

	switch workos.EventKind(event.Event) {
	case workos.EventKindUserCreated, workos.EventKindUserUpdated:
		return p.handleUserUpsert(ctx, dbtx, payload)
	case workos.EventKindUserDeleted:
		return nil, p.handleUserDeleted(ctx, dbtx, payload)
	default:
		return nil, nil
	}
}

func (p *ProcessWorkOSUserEvents) handleUserUpsert(ctx context.Context, dbtx database.DBTX, payload workosUserEventPayload) (*workosUserExternalIDUpdate, error) {
	gramUserID, err := resolveGramUserIDForWorkOSUser(ctx, dbtx, payload)
	if err != nil {
		return nil, err
	}

	if _, err := usersrepo.New(dbtx).UpsertSyncedUser(ctx, usersrepo.UpsertSyncedUserParams{
		ID:              gramUserID,
		Email:           payload.Email,
		DisplayName:     displayNameFromWorkOSUser(payload),
		PhotoUrl:        conv.ToPGTextEmpty(payload.ProfilePictureURL),
		WorkosID:        conv.ToPGText(payload.ID),
		WorkosCreatedAt: conv.ToPGTimestamptz(payload.CreatedAt),
		WorkosUpdatedAt: conv.ToPGTimestamptz(payload.UpdatedAt),
	}); err != nil {
		return nil, fmt.Errorf("upsert synced user: %w", err)
	}

	if payload.ExternalID == "" {
		return &workosUserExternalIDUpdate{workosUserID: payload.ID, externalID: gramUserID}, nil
	}

	return nil, nil
}

func (p *ProcessWorkOSUserEvents) handleUserDeleted(ctx context.Context, dbtx database.DBTX, payload workosUserEventPayload) error {
	if err := usersrepo.New(dbtx).DisableUser(ctx, usersrepo.DisableUserParams{
		WorkosUpdatedAt: conv.ToPGTimestamptz(payload.UpdatedAt),
		WorkosDeletedAt: conv.ToPGTimestamptz(payload.DeletedAt),
		WorkosID:        conv.ToPGText(payload.ID),
	}); err != nil {
		return fmt.Errorf("disable user: %w", err)
	}
	return nil
}

func resolveGramUserIDForWorkOSUser(ctx context.Context, dbtx database.DBTX, payload workosUserEventPayload) (string, error) {
	if payload.ExternalID != "" {
		return payload.ExternalID, nil
	}

	existingID, err := usersrepo.New(dbtx).GetUserIDByWorkosID(ctx, conv.ToPGText(payload.ID))
	switch {
	case err == nil:
		return existingID, nil
	case errors.Is(err, pgx.ErrNoRows):
		return users.UserIDFromWorkOSID(payload.ID), nil
	default:
		return "", fmt.Errorf("get user ID by WorkOS ID: %w", err)
	}
}

func displayNameFromWorkOSUser(payload workosUserEventPayload) string {
	displayName := strings.TrimSpace(strings.Join([]string{payload.FirstName, payload.LastName}, " "))
	if displayName != "" {
		return displayName
	}
	return payload.Email
}
