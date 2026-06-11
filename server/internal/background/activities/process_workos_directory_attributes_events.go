package activities

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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
	Attributes     json.RawMessage `json:"attributes"`
}

type workosDirectoryUserEventPayload struct {
	ID               string          `json:"id"`
	OrganizationID   string          `json:"organization_id"`
	Email            string          `json:"email"`
	CustomAttributes json.RawMessage `json:"custom_attributes"`
	Username         string          `json:"username"`
	FirstName        string          `json:"first_name"`
	LastName         string          `json:"last_name"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type workosDirectoryGroupMembershipEventPayload struct {
	User  workosDirectoryUserEventPayload  `json:"user"`
	Group workosDirectoryGroupEventPayload `json:"group"`
}

func (p *workosDirectoryGroupMembershipEventPayload) UnmarshalJSON(data []byte) error {
	// Alias avoids infinite recursion into this method.
	type alias workosDirectoryGroupMembershipEventPayload
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	*p = workosDirectoryGroupMembershipEventPayload(decoded)
	return nil
}

func handleDirectoryUserEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, eventKind workos.EventKind, raw json.RawMessage) error {
	payload, err := unmarshalDirectoryUserPayload(raw)
	if err != nil {
		return err
	}

	switch eventKind {
	case workos.EventKindDirectorySyncUserCreated, workos.EventKindDirectorySyncUserUpdated:
		return upsertDirectoryUser(ctx, dbtx, payload)
	case workos.EventKindDirectorySyncUserDeleted:
		if _, err := workosrepo.New(dbtx).DeleteDirectoryUserByWorkOSID(ctx, payload.ID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "delete directory user")
		}
		return nil
	default:
		return nil
	}
}

func handleDirectoryGroupEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, eventKind workos.EventKind, raw json.RawMessage) error {
	payload, err := unmarshalDirectoryGroupPayload(raw)
	if err != nil {
		return err
	}

	switch eventKind {
	case workos.EventKindDirectorySyncGroupCreated, workos.EventKindDirectorySyncGroupUpdated:
		return upsertDirectoryGroup(ctx, dbtx, payload)
	case workos.EventKindDirectorySyncGroupDeleted:
		return deleteDirectoryGroup(ctx, logger, dbtx, payload)
	default:
		return nil
	}
}

func handleDirectoryGroupMembershipEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, eventKind workos.EventKind, raw json.RawMessage) error {
	payload, err := unmarshalDirectoryGroupMembershipPayload(raw)
	if err != nil {
		return err
	}

	switch eventKind {
	case workos.EventKindDirectorySyncGroupUserAdded:
		return openDirectoryGroupMembership(ctx, logger, dbtx, payload)
	case workos.EventKindDirectorySyncGroupUserRemoved:
		return closeDirectoryGroupMembership(ctx, logger, dbtx, payload)
	default:
		return nil
	}
}

func unmarshalDirectoryGroupPayload(raw json.RawMessage) (workosDirectoryGroupEventPayload, error) {
	var payload workosDirectoryGroupEventPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, oops.Permanent(oops.E(oops.CodeBadRequest, err, "unmarshal directory group event payload"))
	}
	if payload.ID == "" {
		return payload, oops.Permanent(oops.E(oops.CodeBadRequest, nil, "invalid directory group event payload missing ID"))
	}
	return payload, nil
}

func unmarshalDirectoryUserPayload(raw json.RawMessage) (workosDirectoryUserEventPayload, error) {
	var payload workosDirectoryUserEventPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, oops.Permanent(oops.E(oops.CodeBadRequest, err, "unmarshal directory user event payload"))
	}
	if payload.ID == "" {
		return payload, oops.Permanent(oops.E(oops.CodeBadRequest, nil, "invalid directory user event payload missing ID"))
	}
	return payload, nil
}

func unmarshalDirectoryGroupMembershipPayload(raw json.RawMessage) (workosDirectoryGroupMembershipEventPayload, error) {
	var payload workosDirectoryGroupMembershipEventPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, oops.Permanent(oops.E(oops.CodeBadRequest, err, "unmarshal directory group membership event payload"))
	}
	if payload.User.ID == "" || payload.Group.ID == "" {
		return payload, oops.Permanent(oops.E(oops.CodeBadRequest, nil, "invalid directory group membership event payload missing user or group ID"))
	}
	return payload, nil
}

func upsertDirectoryGroup(ctx context.Context, dbtx database.DBTX, payload workosDirectoryGroupEventPayload) error {
	org, err := organizationsrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get organization by WorkOS ID")
	}

	if _, err := workosrepo.New(dbtx).UpsertDirectoryGroup(ctx, workosrepo.UpsertDirectoryGroupParams{
		OrganizationID:         org.ID,
		WorkosDirectoryGroupID: payload.ID,
		Name:                   payload.Name,
		Attributes:             payload.Attributes,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "upsert directory group")
	}

	return nil
}

func upsertDirectoryUser(ctx context.Context, dbtx database.DBTX, payload workosDirectoryUserEventPayload) error {
	org, err := organizationsrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get organization by WorkOS ID")
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

	if _, err := workosrepo.New(dbtx).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        org.ID,
		UserID:                userID,
		WorkosDirectoryUserID: payload.ID,
		Email:                 conv.ToPGText(email),
		Attributes:            payload.CustomAttributes,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "upsert directory user")
	}
	return nil
}

func deleteDirectoryGroup(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, payload workosDirectoryGroupEventPayload) error {
	groupID, err := workosrepo.New(dbtx).GetDirectoryGroupIDByWorkOSID(ctx, payload.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		logger.WarnContext(ctx, "skipping directory group deletion for unknown group",
			attr.SlogWorkOSDirectoryGroupID(payload.ID),
		)
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get directory group ID by WorkOS ID")
	}

	_, err = workosrepo.New(dbtx).DeleteDirectoryGroupByWorkOSID(ctx, payload.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete directory group")
	}
	if _, err := workosrepo.New(dbtx).CloseDirectoryUserGroupMembershipsForGroup(ctx, groupID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "close directory group memberships")
	}

	return nil
}

func openDirectoryGroupMembership(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, payload workosDirectoryGroupMembershipEventPayload) error {
	userID, err := workosrepo.New(dbtx).GetDirectoryUserIDByWorkOSID(ctx, payload.User.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		logger.WarnContext(ctx, "skipping directory group membership for unknown user",
			attr.SlogWorkOSUserID(payload.User.ID),
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
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "open directory group membership")
	}
	return nil
}

func closeDirectoryGroupMembership(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, payload workosDirectoryGroupMembershipEventPayload) error {
	if _, err := workosrepo.New(dbtx).CloseDirectoryUserGroupMembershipByWorkOSIDs(ctx, workosrepo.CloseDirectoryUserGroupMembershipByWorkOSIDsParams{
		WorkosDirectoryUserID:  payload.User.ID,
		WorkosDirectoryGroupID: payload.Group.ID,
	}); err != nil {
		logger.WarnContext(ctx, "skipping directory group membership for unknown user or group",
			attr.SlogWorkOSDirectoryUserID(payload.User.ID),
			attr.SlogWorkOSDirectoryGroupID(payload.Group.ID),
		)
		return nil
	}
	return nil
}
