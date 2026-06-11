package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
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
	RawPayload     json.RawMessage `json:"-"`
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
	RawPayload       json.RawMessage `json:"-"`
}

type workosDirectoryGroupMembershipEventPayload struct {
	User  workosDirectoryUserEventPayload  `json:"user"`
	Group workosDirectoryGroupEventPayload `json:"group"`
}

func handleDirectoryUserEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, eventID string, eventKind workos.EventKind, raw json.RawMessage) error {
	payload, err := unmarshalDirectoryUserPayload(raw)
	if err != nil {
		return err
	}

	switch eventKind {
	case workos.EventKindDirectorySyncUserCreated, workos.EventKindDirectorySyncUserUpdated:
		_, err := upsertDirectoryUser(ctx, logger, dbtx, payload)
		return err
	case workos.EventKindDirectorySyncUserDeleted:
		if _, err := workosrepo.New(dbtx).DeleteDirectoryUserByWorkOSID(ctx, payload.ID); err != nil {
			return fmt.Errorf("delete directory user %q from event %q: %w", payload.ID, eventID, err)
		}
		return nil
	default:
		return nil
	}
}

func handleDirectoryAttributesEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, eventKind workos.EventKind, raw json.RawMessage) error {
	switch eventKind {
	case workos.EventKindDirectorySyncGroupCreated, workos.EventKindDirectorySyncGroupUpdated:
		payload, err := unmarshalDirectoryGroupPayload(raw)
		if err != nil {
			return err
		}
		_, err = upsertDirectoryGroup(ctx, dbtx, payload)
		return err

	case workos.EventKindDirectorySyncGroupDeleted:
		payload, err := unmarshalDirectoryGroupPayload(raw)
		if err != nil {
			return err
		}
		return deleteDirectoryGroup(ctx, logger, dbtx, payload)

	case workos.EventKindDirectorySyncGroupUserAdded:
		payload, err := unmarshalDirectoryGroupMembershipPayload(raw)
		if err != nil {
			return err
		}
		return openDirectoryGroupMembership(ctx, logger, dbtx, payload)

	case workos.EventKindDirectorySyncGroupUserRemoved:
		payload, err := unmarshalDirectoryGroupMembershipPayload(raw)
		if err != nil {
			return err
		}
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
	payload.RawPayload = append(json.RawMessage(nil), raw...)
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
	payload.RawPayload = append(json.RawMessage(nil), raw...)
	return payload, nil
}

func unmarshalDirectoryGroupMembershipPayload(raw json.RawMessage) (workosDirectoryGroupMembershipEventPayload, error) {
	var payload workosDirectoryGroupMembershipEventPayload
	var rawPayload struct {
		User  json.RawMessage `json:"user"`
		Group json.RawMessage `json:"group"`
	}
	if err := json.Unmarshal(raw, &rawPayload); err != nil {
		return payload, oops.Permanent(oops.E(oops.CodeBadRequest, err, "unmarshal directory group membership event payload"))
	}
	user, err := unmarshalDirectoryUserPayload(rawPayload.User)
	if err != nil {
		return payload, err
	}
	group, err := unmarshalDirectoryGroupPayload(rawPayload.Group)
	if err != nil {
		return payload, err
	}
	payload = workosDirectoryGroupMembershipEventPayload{User: user, Group: group}
	if payload.User.ID == "" || payload.Group.ID == "" {
		return payload, oops.Permanent(oops.E(oops.CodeBadRequest, nil, "invalid directory group membership event payload missing user or group ID"))
	}
	if payload.User.OrganizationID == "" {
		payload.User.OrganizationID = payload.Group.OrganizationID
	}
	if payload.Group.OrganizationID == "" {
		payload.Group.OrganizationID = payload.User.OrganizationID
	}
	return payload, nil
}

func upsertDirectoryGroup(ctx context.Context, dbtx database.DBTX, payload workosDirectoryGroupEventPayload) (uuid.UUID, error) {
	org, err := organizationsrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "get organization by WorkOS ID")
	}

	groupID, err := workosrepo.New(dbtx).UpsertDirectoryGroup(ctx, workosrepo.UpsertDirectoryGroupParams{
		OrganizationID:         org.ID,
		WorkosDirectoryGroupID: payload.ID,
		Name:                   payload.Name,
		Attributes:             payload.RawPayload,
	})
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "upsert directory group")
	}

	return groupID, nil
}

func upsertDirectoryUser(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, payload workosDirectoryUserEventPayload) (uuid.UUID, error) {
	org, err := organizationsrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "get organization by WorkOS ID")
	}

	var userID pgtype.Text
	email := directoryUserEmail(payload)
	if email != "" {
		user, err := usersrepo.New(dbtx).GetUserByEmail(ctx, email)
		switch {
		case err == nil:
			userID = conv.ToPGText(user.ID)
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return uuid.Nil, oops.E(oops.CodeUnexpected, err, "get user by directory email")
		}
	}

	directoryUserID, err := workosrepo.New(dbtx).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        org.ID,
		UserID:                userID,
		WorkosDirectoryUserID: payload.ID,
		Email:                 conv.ToPGText(email),
		Attributes:            payload.RawPayload,
	})
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "upsert directory user")
	}
	if userID.Valid {
		logger.DebugContext(ctx, "linked directory user to existing user by email",
			attr.SlogUserID(userID.String),
			attr.SlogWorkOSUserID(payload.ID),
		)
	}
	return directoryUserID, nil
}

func deleteDirectoryGroup(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, payload workosDirectoryGroupEventPayload) error {
	groupID, err := workosrepo.New(dbtx).GetDirectoryGroupIDByWorkOSID(ctx, payload.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		logger.WarnContext(ctx, "skipping directory group deletion for unknown group",
			attr.SlogWorkOSDirectoryAttributesEntityID(payload.ID),
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
	userID, err := upsertDirectoryUser(ctx, logger, dbtx, payload.User)
	if err != nil {
		return err
	}

	groupID, err := upsertDirectoryGroup(ctx, dbtx, payload.Group)
	if err != nil {
		return err
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
			attr.SlogWorkOSDirectoryAttributesEntityID(payload.User.ID),
			attr.SlogWorkOSDirectoryAttributesEntityID(payload.Group.ID),
		)
		return nil
	}
	return nil
}

func directoryUserEmail(payload workosDirectoryUserEventPayload) string {
	if payload.Email != "" {
		return strings.ToLower(strings.TrimSpace(payload.Email))
	}
	return ""
}
