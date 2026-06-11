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
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
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

	externalIDUpdate, err := p.handleUserEvent(ctx, logger, dbtx, workosUserID, event)
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

func (p *ProcessWorkOSUserEvents) handleUserEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, workosUserID string, event events.Event) (*workosUserExternalIDUpdate, error) {
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
		return p.handleUserUpsert(ctx, logger, dbtx, payload)
	case workos.EventKindUserDeleted:
		return nil, p.handleUserDeleted(ctx, dbtx, payload)
	default:
		return nil, nil
	}
}

func (p *ProcessWorkOSUserEvents) handleUserUpsert(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, payload workosUserEventPayload) (*workosUserExternalIDUpdate, error) {
	userQueries := usersrepo.New(dbtx)
	resolved, err := resolveWorkOSUser(ctx, logger, userQueries, payload)
	if err != nil {
		return nil, err
	}
	if resolved.needsLink {
		if err := linkExistingUserToWorkOS(ctx, userQueries, resolved.userID, payload.ID); err != nil {
			return nil, err
		}
	}

	if _, err := userQueries.UpsertSyncedUser(ctx, usersrepo.UpsertSyncedUserParams{
		ID:              resolved.userID,
		Email:           payload.Email,
		DisplayName:     displayNameFromWorkOSUser(payload),
		PhotoUrl:        conv.ToPGTextEmpty(payload.ProfilePictureURL),
		WorkosID:        conv.ToPGText(payload.ID),
		WorkosCreatedAt: conv.ToPGTimestamptz(payload.CreatedAt),
		WorkosUpdatedAt: conv.ToPGTimestamptz(payload.UpdatedAt),
	}); err != nil {
		return nil, fmt.Errorf("upsert synced user: %w", err)
	}
	if err := linkDirectoryUsersToUser(ctx, dbtx, resolved.userID, payload.Email); err != nil {
		return nil, err
	}

	organizationQueries := organizationsrepo.New(dbtx)
	if err := organizationQueries.LinkRoleAssignmentsToUser(ctx, organizationsrepo.LinkRoleAssignmentsToUserParams{
		UserID:       conv.ToPGText(resolved.userID),
		WorkosUserID: payload.ID,
	}); err != nil {
		return nil, fmt.Errorf("link role assignments to user: %w", err)
	}
	if err := organizationQueries.LinkRelationshipsToUser(ctx, organizationsrepo.LinkRelationshipsToUserParams{
		UserID:       conv.ToPGText(resolved.userID),
		WorkosUserID: conv.ToPGText(payload.ID),
	}); err != nil {
		return nil, fmt.Errorf("link organization relationships to user: %w", err)
	}
	if err := logRoleAssignmentLinkedToDifferentWorkOSUser(ctx, logger, organizationQueries, resolved.userID, payload.ID); err != nil {
		return nil, err
	}

	if resolved.needsExternalIDUpdate {
		return &workosUserExternalIDUpdate{
			workosUserID: payload.ID,
			externalID:   resolved.userID,
		}, nil
	}

	return nil, nil
}

func logRoleAssignmentLinkedToDifferentWorkOSUser(ctx context.Context, logger *slog.Logger, organizationQueries *organizationsrepo.Queries, gramUserID, workosUserID string) error {
	existingAssignment, err := organizationQueries.GetRoleAssignmentLinkedToDifferentWorkOSUser(ctx, organizationsrepo.GetRoleAssignmentLinkedToDifferentWorkOSUserParams{
		UserID:       conv.ToPGText(gramUserID),
		WorkosUserID: workosUserID,
	})
	switch {
	case err == nil:
		logger.WarnContext(ctx, "role assignment already linked to a different WorkOS user",
			attr.SlogUserID(gramUserID),
			attr.SlogWorkOSUserID(workosUserID),
			attr.SlogOrganizationRoleAssignmentID(existingAssignment.ID.String()),
			attr.SlogWorkOSLinkedUserID(existingAssignment.WorkosUserID),
		)
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	default:
		return fmt.Errorf("get role assignment linked to different WorkOS user: %w", err)
	}

	return nil
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

type resolvedWorkOSUser struct {
	userID                string
	needsLink             bool
	needsExternalIDUpdate bool
}

func resolveWorkOSUser(ctx context.Context, logger *slog.Logger, userQueries *usersrepo.Queries, payload workosUserEventPayload) (resolvedWorkOSUser, error) {
	existingID, err := userQueries.GetUserIDByWorkosID(ctx, conv.ToPGText(payload.ID))
	switch {
	case err == nil:
		return resolvedWorkOSUser{
			userID:                existingID,
			needsLink:             false,
			needsExternalIDUpdate: payload.ExternalID == "",
		}, nil
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return resolvedWorkOSUser{}, fmt.Errorf("get user ID by WorkOS ID: %w", err)
	}

	if payload.ExternalID == "" {
		return resolvedWorkOSUser{
			userID:                users.UserIDFromWorkOSID(payload.ID),
			needsLink:             false,
			needsExternalIDUpdate: true,
		}, nil
	}

	existingUser, err := userQueries.GetUser(ctx, payload.ExternalID)
	switch {
	case err == nil:
		if !existingUser.WorkosID.Valid {
			return resolvedWorkOSUser{
				userID:                existingUser.ID,
				needsLink:             true,
				needsExternalIDUpdate: false,
			}, nil
		}
		err := fmt.Errorf("workos user %q external ID %q is already linked to workos user %q", payload.ID, payload.ExternalID, existingUser.WorkosID.String)
		logger.ErrorContext(ctx, "WorkOS user external ID conflict", attr.SlogError(err),
			attr.SlogUserID(existingUser.ID),
			attr.SlogWorkOSUserID(payload.ID),
			attr.SlogWorkOSLinkedUserID(existingUser.WorkosID.String),
		)
		return resolvedWorkOSUser{}, oops.Permanent(err)
	case errors.Is(err, pgx.ErrNoRows):
		return resolvedWorkOSUser{
			userID:                payload.ExternalID,
			needsLink:             false,
			needsExternalIDUpdate: false,
		}, nil
	default:
		return resolvedWorkOSUser{}, fmt.Errorf("get user %q by WorkOS external ID: %w", payload.ExternalID, err)
	}
}

func linkExistingUserToWorkOS(ctx context.Context, userQueries *usersrepo.Queries, userID, workosUserID string) error {
	if err := userQueries.SetUserWorkosID(ctx, usersrepo.SetUserWorkosIDParams{
		ID:       userID,
		WorkosID: conv.ToPGText(workosUserID),
	}); err != nil {
		return fmt.Errorf("link existing user %q to WorkOS user %q: %w", userID, workosUserID, err)
	}

	linkedUserID, err := userQueries.GetUserIDByWorkosID(ctx, conv.ToPGText(workosUserID))
	switch {
	case err == nil && linkedUserID == userID:
		return nil
	case err == nil:
		return fmt.Errorf("link existing user %q to WorkOS user %q: WorkOS user is linked to user %q", userID, workosUserID, linkedUserID)
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("link existing user %q to WorkOS user %q: user was not linked", userID, workosUserID)
	default:
		return fmt.Errorf("get linked user for WorkOS user %q: %w", workosUserID, err)
	}
}

func linkDirectoryUsersToUser(ctx context.Context, dbtx database.DBTX, userID, email string) error {
	email = conv.NormalizeEmail(email)
	if email == "" {
		return nil
	}

	if _, err := workosrepo.New(dbtx).LinkDirectoryUsersToUserByEmail(ctx, workosrepo.LinkDirectoryUsersToUserByEmailParams{
		UserID: conv.ToPGText(userID),
		Email:  conv.ToPGText(email),
	}); err != nil {
		return fmt.Errorf("link directory users to user: %w", err)
	}
	return nil
}

func displayNameFromWorkOSUser(payload workosUserEventPayload) string {
	displayName := strings.TrimSpace(strings.Join([]string{payload.FirstName, payload.LastName}, " "))
	if displayName != "" {
		return displayName
	}
	return payload.Email
}
