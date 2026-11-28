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

	"github.com/speakeasy-api/gram/server/internal/agentownership"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/userblock"
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
	auditLogger  *audit.Logger
	staffOrg     staff.Organization
}

// ProcessWorkOSUserEventsOption configures optional dependencies on
// ProcessWorkOSUserEvents.
type ProcessWorkOSUserEventsOption func(*ProcessWorkOSUserEvents)

// WithProcessWorkOSUserEventsAuditLogger overrides the audit logger used when
// WorkOS user metadata triggers a blacklist cascade.
func WithProcessWorkOSUserEventsAuditLogger(auditLogger *audit.Logger) ProcessWorkOSUserEventsOption {
	return func(p *ProcessWorkOSUserEvents) {
		if auditLogger != nil {
			p.auditLogger = auditLogger
		}
	}
}

// WithProcessWorkOSUserEventsStaffOrganization sets the deployment staff
// organization so blacklist cascades treat its members as staff.
func WithProcessWorkOSUserEventsStaffOrganization(staffOrg staff.Organization) ProcessWorkOSUserEventsOption {
	return func(p *ProcessWorkOSUserEvents) {
		p.staffOrg = staffOrg
	}
}

func NewProcessWorkOSUserEvents(logger *slog.Logger, db *pgxpool.Pool, workosClient WorkOSClient, opts ...ProcessWorkOSUserEventsOption) *ProcessWorkOSUserEvents {
	p := &ProcessWorkOSUserEvents{
		db:           db,
		logger:       logger,
		workosClient: workosClient,
		auditLogger:  audit.NewLogger(),
		staffOrg:     staff.Organization{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p
}

func (p *ProcessWorkOSUserEvents) Do(ctx context.Context, params ProcessWorkOSUserEventsParams) (*ProcessWorkOSUserEventsResult, error) {
	logger := p.logger.With(attr.SlogWorkOSUserID(params.WorkOSUserID))
	if params.WorkOSUserID == "" {
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("missing WorkOS user ID"), "missing WorkOS user ID").LogError(ctx, logger)
	}

	sinceEventID := conv.PtrValOr(params.SinceEventID, "")
	if sinceEventID == "" {
		cursor, err := workosrepo.New(p.db).GetUserSyncLastEventID(ctx, conv.ToPGText(params.WorkOSUserID))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "get user sync cursor").LogError(ctx, logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list WorkOS user events").LogError(ctx, logger)
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
			return lastEventID, oops.E(oops.CodeUnexpected, err, "handle WorkOS user event").LogError(ctx, eventLogger)
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

	outcome, err := p.handleUserEvent(ctx, logger, dbtx, workosUserID, event)
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

	if outcome.blockStateChanged {
		userblock.Forget(p.db)
	}

	if outcome.externalIDUpdate != nil {
		if err := p.workosClient.UpdateUserExternalID(ctx, outcome.externalIDUpdate.workosUserID, outcome.externalIDUpdate.externalID); err != nil {
			logger.WarnContext(ctx, "failed to set WorkOS user external ID", attr.SlogError(err))
		}
	}

	return event.ID, nil
}

type workosUserEventOutcome struct {
	externalIDUpdate  *workosUserExternalIDUpdate
	blockStateChanged bool
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
	// Metadata is the WorkOS user's custom metadata. Staff block a user from
	// the platform by setting workosUserDisabledMetadataKey on it.
	Metadata map[string]string `json:"metadata"`
}

// workosUserDisabledMetadataKey is the WorkOS user metadata key staff set to
// block a user from the platform: "true" sets users.blacklisted_at, "false"
// clears it, and an absent key leaves it unchanged so a block made another
// way survives unrelated profile updates.
const workosUserDisabledMetadataKey = "blacklisted"

// workosUserDisabledState reads the block state staff set in WorkOS metadata.
// ok is false when the metadata does not say.
func workosUserDisabledState(metadata map[string]string) (disabled bool, ok bool) {
	value, present := metadata[workosUserDisabledMetadataKey]
	if !present {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func (p *ProcessWorkOSUserEvents) handleUserEvent(ctx context.Context, logger *slog.Logger, dbtx pgx.Tx, workosUserID string, event events.Event) (workosUserEventOutcome, error) {
	var zero workosUserEventOutcome
	var payload workosUserEventPayload

	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return zero, oops.Permanent(fmt.Errorf("unmarshal user event payload: %w", err))
	}
	if payload.ID == "" {
		return zero, oops.Permanent(fmt.Errorf("invalid user event payload missing ID"))
	}

	if payload.ID != workosUserID {
		return zero, nil
	}

	switch workos.EventKind(event.Event) {
	case workos.EventKindUserCreated, workos.EventKindUserUpdated:
		return p.handleUserUpsert(ctx, logger, dbtx, payload)
	case workos.EventKindUserDeleted:
		return zero, p.handleUserDeleted(ctx, dbtx, payload)
	default:
		return zero, nil
	}
}

func (p *ProcessWorkOSUserEvents) handleUserUpsert(ctx context.Context, logger *slog.Logger, dbtx pgx.Tx, payload workosUserEventPayload) (workosUserEventOutcome, error) {
	var outcome workosUserEventOutcome
	userQueries := usersrepo.New(dbtx)
	resolved, err := resolveWorkOSUser(ctx, logger, userQueries, payload)
	if err != nil {
		return outcome, err
	}
	if resolved.needsLink {
		if err := linkExistingUserToWorkOS(ctx, userQueries, resolved.userID, payload.ID); err != nil {
			return outcome, err
		}
	}

	applied, err := userQueries.UpsertSyncedUser(ctx, usersrepo.UpsertSyncedUserParams{
		ID:              resolved.userID,
		Email:           payload.Email,
		DisplayName:     displayNameFromWorkOSUser(payload),
		PhotoUrl:        conv.ToPGTextEmpty(payload.ProfilePictureURL),
		WorkosID:        conv.ToPGText(payload.ID),
		WorkosCreatedAt: conv.ToPGTimestamptz(payload.CreatedAt),
		WorkosUpdatedAt: conv.ToPGTimestamptz(payload.UpdatedAt),
	})
	if err != nil {
		return outcome, fmt.Errorf("upsert synced user: %w", err)
	}
	if err := linkDirectoryUsersToUser(ctx, dbtx, resolved.userID, payload.ID, payload.Email); err != nil {
		return outcome, err
	}

	organizationQueries := organizationsrepo.New(dbtx)
	if err := organizationQueries.LinkRoleAssignmentsToUser(ctx, organizationsrepo.LinkRoleAssignmentsToUserParams{
		UserID:       conv.ToPGText(resolved.userID),
		WorkosUserID: payload.ID,
	}); err != nil {
		return outcome, fmt.Errorf("link role assignments to user: %w", err)
	}
	if err := organizationQueries.LinkRelationshipsToUser(ctx, organizationsrepo.LinkRelationshipsToUserParams{
		UserID:       conv.ToPGText(resolved.userID),
		WorkosUserID: conv.ToPGText(payload.ID),
	}); err != nil {
		return outcome, fmt.Errorf("link organization relationships to user: %w", err)
	}
	if err := logRoleAssignmentLinkedToDifferentWorkOSUser(ctx, logger, organizationQueries, resolved.userID, payload.ID); err != nil {
		return outcome, err
	}

	// Only an event newer than the stored profile may change the block state,
	// so a replayed older event cannot undo a newer block or unblock. Run after
	// relationship linking so the blacklist cascade sees newly linked orgs.
	if disabled, ok := workosUserDisabledState(payload.Metadata); ok && applied > 0 {
		outcome.blockStateChanged = true
		if disabled {
			if _, err := userblock.BlacklistTx(ctx, dbtx, userblock.BlacklistParams{
				UserID: resolved.userID,
				Actor: userblock.Actor{
					Principal:   urn.NewSystemPrincipal("workos-user-sync"),
					DisplayName: nil,
				},
				Reason:            "workos_metadata",
				StaffOrganization: p.staffOrg,
				AuditLogger:       p.auditLogger,
				Logger:            logger,
			}); err != nil {
				return outcome, fmt.Errorf("cascade user block from workos metadata: %w", err)
			}
			logger.InfoContext(ctx, "blocked user from workos metadata", attr.SlogUserID(resolved.userID), attr.SlogWorkOSUserID(payload.ID))
		} else {
			if err := userQueries.SetUserBlacklisted(ctx, usersrepo.SetUserBlacklistedParams{
				Blacklisted: false,
				ID:          resolved.userID,
			}); err != nil {
				return outcome, fmt.Errorf("clear user block from workos metadata: %w", err)
			}
			logger.InfoContext(ctx, "unblocked user from workos metadata", attr.SlogUserID(resolved.userID), attr.SlogWorkOSUserID(payload.ID))
		}
	}

	if resolved.needsExternalIDUpdate {
		outcome.externalIDUpdate = &workosUserExternalIDUpdate{
			workosUserID: payload.ID,
			externalID:   resolved.userID,
		}
	}

	return outcome, nil
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

func (p *ProcessWorkOSUserEvents) handleUserDeleted(ctx context.Context, dbtx pgx.Tx, payload workosUserEventPayload) error {
	ownerUserIDs, err := usersrepo.New(dbtx).DisableUser(ctx, usersrepo.DisableUserParams{
		WorkosUpdatedAt: conv.ToPGTimestamptz(payload.UpdatedAt),
		WorkosDeletedAt: conv.ToPGTimestamptz(payload.DeletedAt),
		WorkosID:        conv.ToPGText(payload.ID),
	})
	if err != nil {
		return fmt.Errorf("disable user: %w", err)
	}
	for _, ownerUserID := range ownerUserIDs {
		if err := agentownership.LatchOwnerLossByUser(ctx, dbtx, ownerUserID, agentownership.OwnerReassignmentReasonOwnerDeleted, agentownership.SystemActor, nil); err != nil {
			return fmt.Errorf("latch deleted agent owner: %w", err)
		}
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

func linkDirectoryUsersToUser(ctx context.Context, dbtx database.DBTX, userID, workosUserID, email string) error {
	email = conv.NormalizeEmail(email)
	if email == "" {
		return nil
	}

	if _, err := directoryrepo.New(dbtx).LinkDirectoryUsersToUserByEmail(ctx, directoryrepo.LinkDirectoryUsersToUserByEmailParams{
		UserID:       conv.ToPGText(userID),
		Email:        conv.ToPGText(email),
		WorkosUserID: conv.ToPGText(workosUserID),
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
