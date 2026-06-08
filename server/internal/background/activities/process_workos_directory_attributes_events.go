package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
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
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	workosDirectoryAttributesEventsPageSize = 100

	WorkOSDirectoryAttributesEntityTypeGroup           = "group"
	WorkOSDirectoryAttributesEntityTypeGroupMembership = "group_membership"
)

type ProcessWorkOSDirectoryAttributesEventsParams struct {
	EntityType   string  `json:"entity_type"`
	EntityID     string  `json:"entity_id"`
	SinceEventID *string `json:"since_event_id,omitempty"`
}

type ProcessWorkOSDirectoryAttributesEventsResult struct {
	SinceEventID string `json:"since_event_id"`
	LastEventID  string `json:"last_event_id"`
	HasMore      bool   `json:"has_more"`
}

type ProcessWorkOSDirectoryAttributesEvents struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	workosClient WorkOSClient
}

func NewProcessWorkOSDirectoryAttributesEvents(logger *slog.Logger, db *pgxpool.Pool, workosClient WorkOSClient) *ProcessWorkOSDirectoryAttributesEvents {
	return &ProcessWorkOSDirectoryAttributesEvents{
		db:           db,
		logger:       logger,
		workosClient: workosClient,
	}
}

func (p *ProcessWorkOSDirectoryAttributesEvents) Do(ctx context.Context, params ProcessWorkOSDirectoryAttributesEventsParams) (*ProcessWorkOSDirectoryAttributesEventsResult, error) {
	logger := p.logger.With(
		attr.SlogWorkOSDirectoryAttributesEntityType(params.EntityType),
		attr.SlogWorkOSDirectoryAttributesEntityID(params.EntityID),
	)
	if params.EntityType == "" || params.EntityID == "" {
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("missing directory attributes sync target"), "missing directory attributes sync target").Log(ctx, logger)
	}

	sinceEventID := conv.PtrValOr(params.SinceEventID, "")
	if sinceEventID == "" {
		cursor, err := workosrepo.New(p.db).GetDirectoryAttributesSyncLastEventID(ctx, workosrepo.GetDirectoryAttributesSyncLastEventIDParams{
			EntityType: params.EntityType,
			EntityID:   params.EntityID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "get directory attributes sync cursor").Log(ctx, logger)
		default:
			sinceEventID = cursor
		}
	}

	resp, err := p.workosClient.ListEvents(ctx, events.ListEventsOpts{
		Events: []string{
			string(workos.EventKindDirectorySyncGroupCreated),
			string(workos.EventKindDirectorySyncGroupUpdated),
			string(workos.EventKindDirectorySyncGroupDeleted),
			string(workos.EventKindDirectorySyncGroupUserAdded),
			string(workos.EventKindDirectorySyncGroupUserRemoved),
		},
		Limit:          workosDirectoryAttributesEventsPageSize,
		After:          sinceEventID,
		OrganizationId: "",
		RangeStart:     "",
		RangeEnd:       "",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list WorkOS directory attributes events").Log(ctx, logger)
	}

	lastEventID, err := p.handlePage(ctx, logger, params, resp.Data)
	if err != nil {
		return nil, err
	}

	return &ProcessWorkOSDirectoryAttributesEventsResult{
		SinceEventID: sinceEventID,
		LastEventID:  lastEventID,
		HasMore:      len(resp.Data) == workosDirectoryAttributesEventsPageSize,
	}, nil
}

func (p *ProcessWorkOSDirectoryAttributesEvents) handlePage(ctx context.Context, logger *slog.Logger, params ProcessWorkOSDirectoryAttributesEventsParams, page []events.Event) (string, error) {
	var lastEventID string
	for _, event := range page {
		eventLogger := logger.With(
			attr.SlogWorkOSEventID(event.ID),
			attr.SlogWorkOSEventType(event.Event),
		)

		eventID, err := p.handleEvent(ctx, eventLogger, params, event)
		if err != nil {
			return lastEventID, oops.E(oops.CodeUnexpected, err, "handle WorkOS directory attributes event").Log(ctx, eventLogger)
		}
		if eventID != "" {
			lastEventID = eventID
		}
	}
	return lastEventID, nil
}

func (p *ProcessWorkOSDirectoryAttributesEvents) handleEvent(ctx context.Context, logger *slog.Logger, params ProcessWorkOSDirectoryAttributesEventsParams, event events.Event) (string, error) {
	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := handleDirectoryAttributesEvent(ctx, logger, dbtx, params, event); err != nil {
		return "", err
	}

	if _, err := workosrepo.New(dbtx).SetDirectoryAttributesSyncLastEventID(ctx, workosrepo.SetDirectoryAttributesSyncLastEventIDParams{
		EntityType:  params.EntityType,
		EntityID:    params.EntityID,
		LastEventID: event.ID,
	}); err != nil {
		return "", fmt.Errorf("set directory attributes sync cursor: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	return event.ID, nil
}

type workosDirectoryGroupEventPayload struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organization_id"`
	Name           string          `json:"name"`
	RawAttributes  json.RawMessage `json:"raw_attributes"`
	Attributes     json.RawMessage `json:"attributes"`
}

type workosDirectoryGroupMembershipEventPayload struct {
	ID                     string                           `json:"id"`
	DirectoryUserID        string                           `json:"directory_user_id"`
	DirectoryGroupID       string                           `json:"directory_group_id"`
	WorkOSDirectoryUserID  string                           `json:"workos_directory_user_id"`
	WorkOSDirectoryGroupID string                           `json:"workos_directory_group_id"`
	UserID                 string                           `json:"user_id"`
	GroupID                string                           `json:"group_id"`
	User                   workosDirectoryUserEventPayload  `json:"user"`
	DirectoryUser          workosDirectoryUserEventPayload  `json:"directory_user"`
	Group                  workosDirectoryGroupEventPayload `json:"group"`
	DirectoryGroup         workosDirectoryGroupEventPayload `json:"directory_group"`
}

func handleDirectoryAttributesEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, params ProcessWorkOSDirectoryAttributesEventsParams, event events.Event) error {
	switch workos.EventKind(event.Event) {
	case workos.EventKindDirectorySyncGroupCreated, workos.EventKindDirectorySyncGroupUpdated:
		payload, err := unmarshalDirectoryGroupPayload(event.Data)
		if err != nil {
			return err
		}
		if params.EntityType != WorkOSDirectoryAttributesEntityTypeGroup || params.EntityID != payload.ID {
			return nil
		}
		_, err = upsertDirectoryGroup(ctx, dbtx, payload, event.Data)
		return err

	case workos.EventKindDirectorySyncGroupDeleted:
		payload, err := unmarshalDirectoryGroupPayload(event.Data)
		if err != nil {
			return err
		}
		if params.EntityType != WorkOSDirectoryAttributesEntityTypeGroup || params.EntityID != payload.ID {
			return nil
		}
		return deleteDirectoryGroup(ctx, dbtx, payload.ID)

	case workos.EventKindDirectorySyncGroupUserAdded:
		payload, err := unmarshalDirectoryGroupMembershipPayload(event.Data)
		if err != nil {
			return err
		}
		if !matchesDirectoryGroupMembershipTarget(params, payload) {
			return nil
		}
		return openDirectoryGroupMembership(ctx, logger, dbtx, payload, event.Data)

	case workos.EventKindDirectorySyncGroupUserRemoved:
		payload, err := unmarshalDirectoryGroupMembershipPayload(event.Data)
		if err != nil {
			return err
		}
		if !matchesDirectoryGroupMembershipTarget(params, payload) {
			return nil
		}
		return closeDirectoryGroupMembership(ctx, dbtx, payload)

	default:
		return nil
	}
}

func unmarshalDirectoryGroupPayload(raw json.RawMessage) (workosDirectoryGroupEventPayload, error) {
	var payload workosDirectoryGroupEventPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, oops.Permanent(fmt.Errorf("unmarshal directory group event payload: %w", err))
	}
	if payload.ID == "" {
		return payload, oops.Permanent(fmt.Errorf("invalid directory group event payload missing ID"))
	}
	return payload, nil
}

func unmarshalDirectoryGroupMembershipPayload(raw json.RawMessage) (workosDirectoryGroupMembershipEventPayload, error) {
	var payload workosDirectoryGroupMembershipEventPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, oops.Permanent(fmt.Errorf("unmarshal directory group membership event payload: %w", err))
	}
	if payload.directoryUserID() == "" || payload.directoryGroupID() == "" {
		return payload, oops.Permanent(fmt.Errorf("invalid directory group membership event payload missing user or group ID"))
	}
	return payload, nil
}

func upsertDirectoryGroup(ctx context.Context, dbtx database.DBTX, payload workosDirectoryGroupEventPayload, raw json.RawMessage) (uuid.UUID, error) {
	if payload.OrganizationID == "" {
		return uuid.Nil, oops.Permanent(fmt.Errorf("invalid directory group event payload missing organization_id"))
	}
	if payload.Name == "" {
		return uuid.Nil, oops.Permanent(fmt.Errorf("invalid directory group event payload missing name"))
	}

	org, err := organizationsrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	if err != nil {
		return uuid.Nil, fmt.Errorf("get organization by WorkOS ID: %w", err)
	}

	normalized, hash, err := stableJSONHash(raw)
	if err != nil {
		return uuid.Nil, oops.Permanent(fmt.Errorf("hash directory group payload: %w", err))
	}

	groupID, err := workosrepo.New(dbtx).UpsertDirectoryGroup(ctx, workosrepo.UpsertDirectoryGroupParams{
		OrganizationID:         org.ID,
		WorkosDirectoryGroupID: payload.ID,
		Name:                   payload.Name,
		Attributes:             normalized,
		AttributesContentHash:  conv.ToPGText(hash),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return workosrepo.New(dbtx).GetDirectoryGroupIDByWorkOSID(ctx, payload.ID)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert directory group: %w", err)
	}

	return groupID, nil
}

func deleteDirectoryGroup(ctx context.Context, dbtx database.DBTX, workosDirectoryGroupID string) error {
	groupID, err := workosrepo.New(dbtx).GetDirectoryGroupIDByWorkOSID(ctx, workosDirectoryGroupID)
	switch {
	case err == nil:
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	default:
		return fmt.Errorf("get directory group ID: %w", err)
	}

	if _, err := workosrepo.New(dbtx).DeleteDirectoryGroupByWorkOSID(ctx, workosDirectoryGroupID); err != nil {
		return fmt.Errorf("delete directory group: %w", err)
	}
	if _, err := workosrepo.New(dbtx).CloseUserGroupMembershipsForGroup(ctx, groupID); err != nil {
		return fmt.Errorf("close directory group memberships: %w", err)
	}
	return nil
}

func openDirectoryGroupMembership(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, payload workosDirectoryGroupMembershipEventPayload, raw json.RawMessage) error {
	groupPayload, groupRaw, ok := payload.embeddedGroupPayload(raw)
	if !ok {
		return oops.Permanent(fmt.Errorf("directory group membership event missing embedded group"))
	}
	groupID, err := upsertDirectoryGroup(ctx, dbtx, groupPayload, groupRaw)
	if err != nil {
		return err
	}

	userPayload, userRaw, ok := payload.embeddedUserPayload(raw)
	if !ok {
		return oops.Permanent(fmt.Errorf("directory group membership event missing embedded user"))
	}
	userID, err := storeDirectoryUserAttributes(ctx, logger, dbtx, userPayload.ID, userPayload, userRaw)
	if err != nil {
		return err
	}
	if userID == "" {
		return nil
	}

	if _, err := workosrepo.New(dbtx).OpenUserGroupMembership(ctx, workosrepo.OpenUserGroupMembershipParams{
		UserID:                 userID,
		GroupID:                groupID,
		WorkosDirectoryUserID:  payload.directoryUserID(),
		WorkosDirectoryGroupID: payload.directoryGroupID(),
	}); err != nil {
		return fmt.Errorf("open directory group membership: %w", err)
	}
	return nil
}

func closeDirectoryGroupMembership(ctx context.Context, dbtx database.DBTX, payload workosDirectoryGroupMembershipEventPayload) error {
	if _, err := workosrepo.New(dbtx).CloseUserGroupMembershipByWorkOSIDs(ctx, workosrepo.CloseUserGroupMembershipByWorkOSIDsParams{
		WorkosDirectoryUserID:  payload.directoryUserID(),
		WorkosDirectoryGroupID: payload.directoryGroupID(),
	}); err != nil {
		return fmt.Errorf("close directory group membership: %w", err)
	}
	return nil
}

func storeDirectoryUserAttributes(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, workosDirectoryUserID string, payload workosDirectoryUserEventPayload, raw json.RawMessage) (string, error) {
	if payload.ID == "" {
		return "", oops.Permanent(fmt.Errorf("invalid directory user event payload missing ID"))
	}
	if payload.ID != workosDirectoryUserID {
		return "", nil
	}

	email := directoryUserEmail(payload)
	if email == "" {
		logger.WarnContext(ctx, "skipping directory user attributes without resolvable email",
			attr.SlogWorkOSUserID(workosDirectoryUserID),
		)
		return "", nil
	}

	userQueries := usersrepo.New(dbtx)
	user, err := userQueries.GetUserByEmail(ctx, email)
	switch {
	case err == nil:
	case errors.Is(err, pgx.ErrNoRows):
		logger.WarnContext(ctx, "skipping directory user attributes for unknown email",
			attr.SlogWorkOSUserID(workosDirectoryUserID),
		)
		return "", nil
	default:
		return "", fmt.Errorf("get user by directory user email: %w", err)
	}

	normalized, hash, err := stableJSONHash(raw)
	if err != nil {
		return "", oops.Permanent(fmt.Errorf("hash directory user payload: %w", err))
	}

	if _, err := userQueries.UpdateUserDirectoryAttributesByID(ctx, usersrepo.UpdateUserDirectoryAttributesByIDParams{
		ID:                    user.ID,
		Attributes:            normalized,
		AttributesContentHash: conv.ToPGText(hash),
	}); err != nil {
		return "", fmt.Errorf("update user directory attributes: %w", err)
	}

	return user.ID, nil
}

func matchesDirectoryGroupMembershipTarget(params ProcessWorkOSDirectoryAttributesEventsParams, payload workosDirectoryGroupMembershipEventPayload) bool {
	return params.EntityType == WorkOSDirectoryAttributesEntityTypeGroupMembership &&
		params.EntityID == DirectoryGroupMembershipSyncEntityID(payload.directoryGroupID(), payload.directoryUserID())
}

func DirectoryGroupMembershipSyncEntityID(workosDirectoryGroupID, workosDirectoryUserID string) string {
	return strings.Join([]string{workosDirectoryGroupID, workosDirectoryUserID}, ":")
}

func (p workosDirectoryGroupMembershipEventPayload) directoryUserID() string {
	for _, candidate := range []string{p.DirectoryUserID, p.WorkOSDirectoryUserID, p.UserID, p.User.ID, p.DirectoryUser.ID} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func (p workosDirectoryGroupMembershipEventPayload) directoryGroupID() string {
	for _, candidate := range []string{p.DirectoryGroupID, p.WorkOSDirectoryGroupID, p.GroupID, p.Group.ID, p.DirectoryGroup.ID} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func (p workosDirectoryGroupMembershipEventPayload) embeddedUserPayload(raw json.RawMessage) (workosDirectoryUserEventPayload, json.RawMessage, bool) {
	if p.User.ID != "" {
		return p.User, rawObjectField(raw, "user"), true
	}
	if p.DirectoryUser.ID != "" {
		return p.DirectoryUser, rawObjectField(raw, "directory_user"), true
	}
	return workosDirectoryUserEventPayload{}, nil, false
}

func (p workosDirectoryGroupMembershipEventPayload) embeddedGroupPayload(raw json.RawMessage) (workosDirectoryGroupEventPayload, json.RawMessage, bool) {
	if p.Group.ID != "" {
		return p.Group, rawObjectField(raw, "group"), true
	}
	if p.DirectoryGroup.ID != "" {
		return p.DirectoryGroup, rawObjectField(raw, "directory_group"), true
	}
	return workosDirectoryGroupEventPayload{}, nil, false
}

func rawObjectField(raw json.RawMessage, field string) json.RawMessage {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil
	}
	return object[field]
}
