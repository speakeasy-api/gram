package activities

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/workos/workos-go/v6/pkg/directorysync"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type workosDirectoryGroupEventPayload struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organization_id"`
	Name           string          `json:"name"`
	RawAttributes  json.RawMessage `json:"raw_attributes"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type workosDirectoryUserEventPayload struct {
	ID               string          `json:"id"`
	OrganizationID   string          `json:"organization_id"`
	Email            string          `json:"email"`
	CustomAttributes json.RawMessage `json:"custom_attributes"`
	Username         string          `json:"username"`
	FirstName        string          `json:"first_name"`
	LastName         string          `json:"last_name"`
	State            string          `json:"state"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type workosDirectoryGroupMembershipEventPayload struct {
	User  workosDirectoryUserEventPayload  `json:"user"`
	Group workosDirectoryGroupEventPayload `json:"group"`
}

// handleDirectoryUserEvent applies a dsync.user.* event. It returns the Gram
// user ID whose organization access was deprovisioned (empty when no access
// changed) so the caller can invalidate cached user info after commit.
func handleDirectoryUserEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) (string, error) {
	var payload workosDirectoryUserEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return "", oops.Permanent(oops.E(oops.CodeBadRequest, err, "unmarshal directory user event payload"))
	}
	if payload.ID == "" {
		return "", oops.Permanent(oops.E(oops.CodeBadRequest, nil, "invalid directory user event payload missing ID"))
	}

	switch workos.EventKind(event.Event) {
	case workos.EventKindDirectorySyncUserCreated, workos.EventKindDirectorySyncUserUpdated:
		// A non-active state on a create/update is how SCIM deactivation
		// (e.g. suspending a user in the IdP) surfaces: WorkOS emits
		// dsync.user.updated with state=inactive rather than a delete event.
		if payload.State != "" && payload.State != string(directorysync.Active) {
			return deactivateDirectoryUser(ctx, logger, dbtx, event, payload)
		}
		return "", upsertDirectoryUser(ctx, dbtx, event, payload)
	case workos.EventKindDirectorySyncUserDeleted:
		existing, err := workosrepo.New(dbtx).GetDirectoryUserSyncStateByWorkOSID(ctx, payload.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "get directory user sync state")
		}
		var rowUpdatedAt *time.Time
		if existing.WorkosUpdatedAt.Valid {
			rowUpdatedAt = &existing.WorkosUpdatedAt.Time
		}
		eventUpdatedAt := conv.Default(payload.UpdatedAt, event.CreatedAt)
		if !ShouldProcessEvent(conv.FromPGText[string](existing.WorkosLastEventID), rowUpdatedAt, event.ID, eventUpdatedAt) {
			return "", nil
		}
		if _, err := workosrepo.New(dbtx).DeleteDirectoryUserByWorkOSID(ctx, workosrepo.DeleteDirectoryUserByWorkOSIDParams{
			WorkosDeletedAt:       conv.ToPGTimestamptz(eventUpdatedAt),
			WorkosLastEventID:     conv.ToPGText(event.ID),
			WorkosDirectoryUserID: payload.ID,
		}); err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "delete directory user")
		}
		return "", nil
	default:
		return "", nil
	}
}

func handleDirectoryGroupEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosDirectoryGroupEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return oops.Permanent(oops.E(oops.CodeBadRequest, err, "unmarshal directory group event payload"))
	}
	if payload.ID == "" {
		return oops.Permanent(oops.E(oops.CodeBadRequest, nil, "invalid directory group event payload missing ID"))
	}

	switch workos.EventKind(event.Event) {
	case workos.EventKindDirectorySyncGroupCreated, workos.EventKindDirectorySyncGroupUpdated:
		return upsertDirectoryGroup(ctx, dbtx, event, payload)
	case workos.EventKindDirectorySyncGroupDeleted:
		return deleteDirectoryGroup(ctx, logger, dbtx, event, payload)
	default:
		return nil
	}
}

func handleDirectoryGroupMembershipEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event) error {
	var payload workosDirectoryGroupMembershipEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return oops.Permanent(oops.E(oops.CodeBadRequest, err, "unmarshal directory group membership event payload"))
	}
	if payload.User.ID == "" || payload.Group.ID == "" {
		return oops.Permanent(oops.E(oops.CodeBadRequest, nil, "invalid directory group membership event payload missing user or group ID"))
	}

	switch workos.EventKind(event.Event) {
	case workos.EventKindDirectorySyncGroupUserAdded:
		return openDirectoryGroupMembership(ctx, logger, dbtx, event, payload)
	case workos.EventKindDirectorySyncGroupUserRemoved:
		return closeDirectoryGroupMembership(ctx, logger, dbtx, event, payload)
	default:
		return nil
	}
}

func upsertDirectoryGroup(ctx context.Context, dbtx database.DBTX, event events.Event, payload workosDirectoryGroupEventPayload) error {
	org, err := organizationsrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get organization by WorkOS ID")
	}

	existing, err := workosrepo.New(dbtx).GetDirectoryGroupSyncStateByWorkOSID(ctx, payload.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeUnexpected, err, "get directory group sync state")
	}
	if err == nil {
		var rowUpdatedAt *time.Time
		if existing.WorkosUpdatedAt.Valid {
			rowUpdatedAt = &existing.WorkosUpdatedAt.Time
		}
		if !ShouldProcessEvent(conv.FromPGText[string](existing.WorkosLastEventID), rowUpdatedAt, event.ID, conv.Default(payload.UpdatedAt, event.CreatedAt)) {
			return nil
		}
	}

	attributes := payload.RawAttributes
	if len(attributes) == 0 || string(attributes) == "null" {
		attributes = []byte("{}")
	}
	if _, err := workosrepo.New(dbtx).UpsertDirectoryGroup(ctx, workosrepo.UpsertDirectoryGroupParams{
		OrganizationID:         org.ID,
		WorkosDirectoryGroupID: payload.ID,
		Name:                   payload.Name,
		// Groups have no custom_attributes equivalent; raw_attributes is the
		// only attribute payload WorkOS sends for directory groups.
		Attributes:        attributes,
		WorkosCreatedAt:   conv.ToPGTimestamptz(conv.Default(payload.CreatedAt, event.CreatedAt)),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(conv.Default(payload.UpdatedAt, event.CreatedAt)),
		WorkosLastEventID: conv.ToPGText(event.ID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "upsert directory group")
	}

	return nil
}

func upsertDirectoryUser(ctx context.Context, dbtx database.DBTX, event events.Event, payload workosDirectoryUserEventPayload) error {
	org, err := organizationsrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get organization by WorkOS ID")
	}

	existing, err := workosrepo.New(dbtx).GetDirectoryUserSyncStateByWorkOSID(ctx, payload.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeUnexpected, err, "get directory user sync state")
	}
	if err == nil {
		var rowUpdatedAt *time.Time
		if existing.WorkosUpdatedAt.Valid {
			rowUpdatedAt = &existing.WorkosUpdatedAt.Time
		}
		if !ShouldProcessEvent(conv.FromPGText[string](existing.WorkosLastEventID), rowUpdatedAt, event.ID, conv.Default(payload.UpdatedAt, event.CreatedAt)) {
			return nil
		}
	}

	var userID pgtype.Text
	email := conv.NormalizeEmail(payload.Email)
	if email != "" {
		user, err := usersrepo.New(dbtx).GetUserByEmail(ctx, email)
		switch {
		case err == nil:
			userID = conv.ToPGText(user.ID)
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return oops.E(oops.CodeUnexpected, err, "get user by directory email")
		}
	}

	attributes := payload.CustomAttributes
	if len(attributes) == 0 || string(attributes) == "null" {
		attributes = []byte("{}")
	}
	if _, err := workosrepo.New(dbtx).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        org.ID,
		UserID:                userID,
		WorkosDirectoryUserID: payload.ID,
		Email:                 conv.ToPGText(email),
		Attributes:            attributes,
		// Only an explicitly active state may resurrect a soft-deleted row.
		// This keeps legitimate re-provisioning working while preventing
		// stateless upserts from restoring a deactivated directory user.
		RestoreDeleted:    payload.State == string(directorysync.Active),
		WorkosCreatedAt:   conv.ToPGTimestamptz(conv.Default(payload.CreatedAt, event.CreatedAt)),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(conv.Default(payload.UpdatedAt, event.CreatedAt)),
		WorkosLastEventID: conv.ToPGText(event.ID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "upsert directory user")
	}
	return nil
}

// deactivateDirectoryUser handles a dsync.user.{created,updated} event whose
// state is not active. It soft-deletes the directory user row and, when the
// directory user maps to a Gram user, deprovisions that user's access to the
// organization (relationship + role assignments), mirroring what an
// organization_membership.deleted event does. Returns the Gram user ID whose
// access was removed, or empty when nothing was deprovisioned.
func deactivateDirectoryUser(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event, payload workosDirectoryUserEventPayload) (string, error) {
	org, err := organizationsrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "get organization by WorkOS ID")
	}

	eventUpdatedAt := conv.Default(payload.UpdatedAt, event.CreatedAt)

	existing, err := workosrepo.New(dbtx).GetDirectoryUserSyncStateByWorkOSID(ctx, payload.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", oops.E(oops.CodeUnexpected, err, "get directory user sync state")
	}
	if err == nil {
		var rowUpdatedAt *time.Time
		if existing.WorkosUpdatedAt.Valid {
			rowUpdatedAt = &existing.WorkosUpdatedAt.Time
		}
		if !ShouldProcessEvent(conv.FromPGText[string](existing.WorkosLastEventID), rowUpdatedAt, event.ID, eventUpdatedAt) {
			return "", nil
		}
	}

	// Resolve the linked Gram user before soft-deleting the directory row:
	// email is the canonical linkage (mirroring upsertDirectoryUser), with the
	// stored user_id as a fallback for directory users whose email changed.
	var gramUserID string
	if email := conv.NormalizeEmail(payload.Email); email != "" {
		user, err := usersrepo.New(dbtx).GetUserByEmail(ctx, email)
		switch {
		case err == nil:
			gramUserID = user.ID
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return "", oops.E(oops.CodeUnexpected, err, "get user by directory email")
		}
	}
	if gramUserID == "" {
		directoryUser, err := workosrepo.New(dbtx).GetDirectoryUserByWorkOSID(ctx, payload.ID)
		switch {
		case err == nil && directoryUser.UserID.Valid:
			gramUserID = directoryUser.UserID.String
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return "", oops.E(oops.CodeUnexpected, err, "get directory user by WorkOS ID")
		}
	}

	if _, err := workosrepo.New(dbtx).DeleteDirectoryUserByWorkOSID(ctx, workosrepo.DeleteDirectoryUserByWorkOSIDParams{
		WorkosDeletedAt:       conv.ToPGTimestamptz(eventUpdatedAt),
		WorkosLastEventID:     conv.ToPGText(event.ID),
		WorkosDirectoryUserID: payload.ID,
	}); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "deactivate directory user")
	}

	if gramUserID == "" {
		logger.WarnContext(ctx, "directory user deactivated but no linked Gram user found",
			attr.SlogWorkOSDirectoryUserID(payload.ID),
		)
		return "", nil
	}

	rel, err := organizationsrepo.New(dbtx).GetOrganizationRelationshipForUser(ctx, organizationsrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: org.ID,
		UserID:         conv.ToPGText(gramUserID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	case err != nil:
		return "", oops.E(oops.CodeUnexpected, err, "get organization relationship for directory user")
	}
	if rel.Deleted {
		return "", nil
	}

	var relUpdatedAt *time.Time
	if rel.WorkosUpdatedAt.Valid {
		relUpdatedAt = &rel.WorkosUpdatedAt.Time
	}
	if !ShouldProcessEvent(conv.FromPGText[string](rel.WorkosLastEventID), relUpdatedAt, event.ID, eventUpdatedAt) {
		return "", nil
	}

	if err := organizationsrepo.New(dbtx).MarkWorkOSMembershipDeleted(ctx, organizationsrepo.MarkWorkOSMembershipDeletedParams{
		OrganizationID:     org.ID,
		UserID:             conv.ToPGText(gramUserID),
		WorkosUserID:       rel.WorkosUserID,
		WorkosMembershipID: rel.WorkosMembershipID,
		WorkosUpdatedAt:    conv.ToPGTimestamptz(eventUpdatedAt),
		WorkosLastEventID:  conv.ToPGText(event.ID),
	}); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "mark organization membership deleted for directory user")
	}

	workosUserID := rel.WorkosUserID.String
	if workosUserID == "" {
		user, err := usersrepo.New(dbtx).GetUser(ctx, gramUserID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", oops.E(oops.CodeUnexpected, err, "get user for directory deactivation")
		}
		if err == nil && user.WorkosID.Valid {
			workosUserID = user.WorkosID.String
		}
	}
	if workosUserID != "" {
		if err := organizationsrepo.New(dbtx).MarkRoleAssignmentsDeleted(ctx, organizationsrepo.MarkRoleAssignmentsDeletedParams{
			OrganizationID:    org.ID,
			WorkosUserID:      workosUserID,
			WorkosUpdatedAt:   conv.ToPGTimestamptz(eventUpdatedAt),
			WorkosLastEventID: conv.ToPGText(event.ID),
		}); err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "mark role assignments deleted for directory user")
		}
	}

	logger.InfoContext(ctx, "deprovisioned organization access for deactivated directory user",
		attr.SlogUserID(gramUserID),
		attr.SlogWorkOSDirectoryUserID(payload.ID),
	)
	return gramUserID, nil
}

func deleteDirectoryGroup(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event, payload workosDirectoryGroupEventPayload) error {
	existing, err := workosrepo.New(dbtx).GetDirectoryGroupSyncStateByWorkOSID(ctx, payload.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		logger.WarnContext(ctx, "skipping directory group deletion for unknown group",
			attr.SlogWorkOSDirectoryGroupID(payload.ID),
		)
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get directory group sync state")
	}
	var rowUpdatedAt *time.Time
	if existing.WorkosUpdatedAt.Valid {
		rowUpdatedAt = &existing.WorkosUpdatedAt.Time
	}
	if !ShouldProcessEvent(conv.FromPGText[string](existing.WorkosLastEventID), rowUpdatedAt, event.ID, conv.Default(payload.UpdatedAt, event.CreatedAt)) {
		return nil
	}

	_, err = workosrepo.New(dbtx).DeleteDirectoryGroupByWorkOSID(ctx, workosrepo.DeleteDirectoryGroupByWorkOSIDParams{
		WorkosDeletedAt:        conv.ToPGTimestamptz(conv.Default(payload.UpdatedAt, event.CreatedAt)),
		WorkosLastEventID:      conv.ToPGText(event.ID),
		WorkosDirectoryGroupID: payload.ID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete directory group")
	}
	if _, err := workosrepo.New(dbtx).CloseDirectoryUserGroupMembershipsForGroup(ctx, existing.ID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "close directory group memberships")
	}

	return nil
}

func openDirectoryGroupMembership(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event, payload workosDirectoryGroupMembershipEventPayload) error {
	latest, err := workosrepo.New(dbtx).GetLatestDirectoryUserGroupMembershipByWorkOSIDs(ctx, workosrepo.GetLatestDirectoryUserGroupMembershipByWorkOSIDsParams{
		WorkosDirectoryUserID:  payload.User.ID,
		WorkosDirectoryGroupID: payload.Group.ID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeUnexpected, err, "get latest directory group membership")
	}
	if err == nil && event.CreatedAt.Before(latest.WorkosCreatedAt.Time) {
		return nil
	}

	userID, err := workosrepo.New(dbtx).GetDirectoryUserIDByWorkOSID(ctx, payload.User.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		logger.WarnContext(ctx, "skipping directory group membership for unknown user",
			attr.SlogWorkOSDirectoryUserID(payload.User.ID),
		)
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get directory user ID by WorkOS ID")
	}

	groupID, err := workosrepo.New(dbtx).GetDirectoryGroupIDByWorkOSID(ctx, payload.Group.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		logger.WarnContext(ctx, "skipping directory group membership for unknown group",
			attr.SlogWorkOSDirectoryGroupID(payload.Group.ID),
		)
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get directory group ID by WorkOS ID")
	}

	if _, err := workosrepo.New(dbtx).OpenDirectoryUserGroupMembership(ctx, workosrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        userID,
		DirectoryGroupID:       groupID,
		WorkosDirectoryUserID:  payload.User.ID,
		WorkosDirectoryGroupID: payload.Group.ID,
		WorkosCreatedAt:        conv.ToPGTimestamptz(event.CreatedAt),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "open directory group membership")
	}
	return nil
}

func closeDirectoryGroupMembership(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, event events.Event, payload workosDirectoryGroupMembershipEventPayload) error {
	latest, err := workosrepo.New(dbtx).GetLatestDirectoryUserGroupMembershipByWorkOSIDs(ctx, workosrepo.GetLatestDirectoryUserGroupMembershipByWorkOSIDsParams{
		WorkosDirectoryUserID:  payload.User.ID,
		WorkosDirectoryGroupID: payload.Group.ID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeUnexpected, err, "get latest directory group membership")
	}
	if err == nil && event.CreatedAt.Before(latest.WorkosCreatedAt.Time) {
		return nil
	}

	rowsAffected, err := workosrepo.New(dbtx).CloseDirectoryUserGroupMembershipByWorkOSIDs(ctx, workosrepo.CloseDirectoryUserGroupMembershipByWorkOSIDsParams{
		WorkosCreatedAt:        conv.ToPGTimestamptz(event.CreatedAt),
		WorkosDirectoryUserID:  payload.User.ID,
		WorkosDirectoryGroupID: payload.Group.ID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "close directory group membership")
	}
	if rowsAffected == 0 {
		logger.WarnContext(ctx, "skipping directory group membership removal for unknown membership",
			attr.SlogWorkOSDirectoryUserID(payload.User.ID),
			attr.SlogWorkOSDirectoryGroupID(payload.Group.ID),
		)
	}
	return nil
}
