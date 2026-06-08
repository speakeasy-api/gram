package activities

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

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
)

const (
	workosDirectoryAttributesEventsPageSize = 100

	WorkOSDirectoryAttributesEntityTypeGroup           = "group"
	WorkOSDirectoryAttributesEntityTypeGroupMembership = "group_membership"
)

type ProcessWorkOSDirectoryAttributesEventsParams struct {
	EntityType             string  `json:"entity_type"`
	WorkOSDirectoryGroupID string  `json:"workos_directory_group_id,omitempty"`
	WorkOSDirectoryUserID  string  `json:"workos_directory_user_id,omitempty"`
	SinceEventID           *string `json:"since_event_id,omitempty"`
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
	syncEntityID, err := directoryAttributesSyncEntityID(params)
	logger := p.logger.With(
		attr.SlogWorkOSDirectoryAttributesEntityType(params.EntityType),
		attr.SlogWorkOSDirectoryAttributesEntityID(syncEntityID),
	)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "missing directory attributes sync target").Log(ctx, logger)
	}

	sinceEventID := conv.PtrValOr(params.SinceEventID, "")
	if sinceEventID == "" {
		cursor, err := workosrepo.New(p.db).GetDirectoryAttributesSyncLastEventID(ctx, workosrepo.GetDirectoryAttributesSyncLastEventIDParams{
			EntityType: params.EntityType,
			EntityID:   syncEntityID,
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
	syncEntityID, err := directoryAttributesSyncEntityID(params)
	if err != nil {
		return "", err
	}

	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "begin transaction")
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := handleDirectoryAttributesEvent(ctx, logger, dbtx, params, event); err != nil {
		return "", err
	}

	if _, err := workosrepo.New(dbtx).SetDirectoryAttributesSyncLastEventID(ctx, workosrepo.SetDirectoryAttributesSyncLastEventIDParams{
		EntityType:  params.EntityType,
		EntityID:    syncEntityID,
		LastEventID: event.ID,
	}); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "set directory attributes sync cursor")
	}

	if err := dbtx.Commit(ctx); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "commit transaction")
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
	User  workosDirectoryUserEventPayload  `json:"user"`
	Group workosDirectoryGroupEventPayload `json:"group"`
}

func handleDirectoryAttributesEvent(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, params ProcessWorkOSDirectoryAttributesEventsParams, event events.Event) error {
	var entityID string
	switch workos.EventKind(event.Event) {
	case workos.EventKindDirectorySyncGroupCreated, workos.EventKindDirectorySyncGroupUpdated:
		payload, err := unmarshalDirectoryGroupPayload(event.Data)
		if err != nil {
			return err
		}
		if params.EntityType != WorkOSDirectoryAttributesEntityTypeGroup || params.WorkOSDirectoryGroupID != payload.ID {
			return nil
		}
		entityID, err = upsertDirectoryGroup(ctx, dbtx, payload)
		return err

	case workos.EventKindDirectorySyncGroupDeleted:
		payload, err := unmarshalDirectoryGroupPayload(event.Data)
		if err != nil {
			return err
		}
		if params.EntityType != WorkOSDirectoryAttributesEntityTypeGroup || params.WorkOSDirectoryGroupID != payload.ID {
			return nil
		}
		entityID = payload.ID
		return deleteDirectoryGroup(ctx, logger, dbtx, payload)

	case workos.EventKindDirectorySyncGroupUserAdded:
		payload, err := unmarshalDirectoryGroupMembershipPayload(event.Data)
		if err != nil {
			return err
		}
		if !matchesDirectoryGroupMembershipTarget(params, payload) {
			return nil
		}
		entityID, err = openDirectoryGroupMembership(ctx, logger, dbtx, payload)
		if err != nil {
			return err
		}
		if entityID == "" {
			return nil
		}

		if _, err := workosrepo.New(dbtx).SetDirectoryAttributesSyncLastEventID(ctx, workosrepo.SetDirectoryAttributesSyncLastEventIDParams{
			EntityType:  params.EntityType,
			EntityID:    entityID,
			LastEventID: event.ID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "set directory attributes sync cursor")
		}

	case workos.EventKindDirectorySyncGroupUserRemoved:
		payload, err := unmarshalDirectoryGroupMembershipPayload(event.Data)
		if err != nil {
			return err
		}
		if !matchesDirectoryGroupMembershipTarget(params, payload) {
			return nil
		}
		entityID, err = closeDirectoryGroupMembership(ctx, logger, dbtx, payload)
		if err != nil {
			return err
		}
		if entityID == "" {
			return nil
		}

		if _, err := workosrepo.New(dbtx).SetDirectoryAttributesSyncLastEventID(ctx, workosrepo.SetDirectoryAttributesSyncLastEventIDParams{
			EntityType:  params.EntityType,
			EntityID:    entityID,
			LastEventID: event.ID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "set directory attributes sync cursor")
		}

	default:
	}

	if entityID != "" {
		if _, err := workosrepo.New(dbtx).SetDirectoryAttributesSyncLastEventID(ctx, workosrepo.SetDirectoryAttributesSyncLastEventIDParams{
			EntityType:  params.EntityType,
			EntityID:    entityID,
			LastEventID: event.ID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "set directory attributes sync cursor")
		}
	}
	return nil
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

func upsertDirectoryGroup(ctx context.Context, dbtx database.DBTX, payload workosDirectoryGroupEventPayload) (string, error) {
	org, err := organizationsrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "get organization by WorkOS ID")
	}

	hash, err := stableJSONHash(payload.RawAttributes)
	if err != nil {
		return "", oops.Permanent(oops.E(oops.CodeBadRequest, err, "hash directory group raw attributes"))
	}

	groupID, err := workosrepo.New(dbtx).UpsertDirectoryGroup(ctx, workosrepo.UpsertDirectoryGroupParams{
		OrganizationID:         org.ID,
		WorkosDirectoryGroupID: payload.ID,
		Name:                   payload.Name,
		Attributes:             payload.RawAttributes,
		AttributesContentHash:  conv.ToPGText(hash),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		groupID, err = workosrepo.New(dbtx).GetDirectoryGroupIDByWorkOSID(ctx, payload.ID)
		if err != nil {
			return "", nil
		}
		return "", nil
	}
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "upsert directory group")
	}

	return groupID.String(), nil
}

func upsertDirectoryUser(ctx context.Context, dbtx database.DBTX, payload workosDirectoryUserEventPayload) (uuid.UUID, error) {
	org, err := organizationsrepo.New(dbtx).GetOrganizationByWorkosID(ctx, conv.ToPGText(payload.OrganizationID))
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "get organization by WorkOS ID")
	}

	hash, err := stableJSONHash(payload.CustomAttributes)
	if err != nil {
		return uuid.Nil, oops.Permanent(oops.E(oops.CodeBadRequest, err, "hash directory user custom attributes"))
	}

	directoryUserID, err := workosrepo.New(dbtx).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        org.ID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: payload.ID,
		Email:                 conv.ToPGText(payload.Email),
		Attributes:            payload.CustomAttributes,
		AttributesContentHash: conv.ToPGText(hash),
	})
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "upsert directory user")
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

func openDirectoryGroupMembership(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, payload workosDirectoryGroupMembershipEventPayload) (string, error) {
	userID, err := workosrepo.New(dbtx).GetDirectoryUserIDByWorkOSID(ctx, payload.User.ID)
	switch {
	case err == nil:
	case errors.Is(err, pgx.ErrNoRows):
		logger.WarnContext(ctx, "skipping directory group membership for unknown user",
			attr.SlogWorkOSDirectoryAttributesEntityID(payload.User.ID),
		)
		return "", nil
	default:
		return "", oops.E(oops.CodeUnexpected, err, "get directory user ID by WorkOS ID")
	}

	groupID, err := workosrepo.New(dbtx).GetDirectoryGroupIDByWorkOSID(ctx, payload.Group.ID)
	switch {
	case err == nil:
	case errors.Is(err, pgx.ErrNoRows):
		logger.WarnContext(ctx, "skipping directory group membership for unknown group",
			attr.SlogWorkOSDirectoryAttributesEntityID(payload.Group.ID),
		)
		return "", nil
	default:
		return "", oops.E(oops.CodeUnexpected, err, "get directory group ID by WorkOS ID")
	}

	membershipID, err := workosrepo.New(dbtx).OpenDirectoryUserGroupMembership(ctx, workosrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        userID,
		DirectoryGroupID:       groupID,
		WorkosDirectoryUserID:  payload.User.ID,
		WorkosDirectoryGroupID: payload.Group.ID,
	})
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "open directory group membership")
	}
	return membershipID.String(), nil

}

func closeDirectoryGroupMembership(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, payload workosDirectoryGroupMembershipEventPayload) (string, error) {
	membershipID, err := workosrepo.New(dbtx).CloseDirectoryUserGroupMembershipByWorkOSIDs(ctx, workosrepo.CloseDirectoryUserGroupMembershipByWorkOSIDsParams{
		WorkosDirectoryUserID:  payload.User.ID,
		WorkosDirectoryGroupID: payload.Group.ID,
	})
	if err != nil {
		logger.WarnContext(ctx, "skipping directory group membership for unknown user or group",
			attr.SlogWorkOSDirectoryAttributesEntityID(payload.User.ID),
			attr.SlogWorkOSDirectoryAttributesEntityID(payload.Group.ID),
		)
		return "", nil
	}
	return membershipID.String(), nil
}

func matchesDirectoryGroupMembershipTarget(params ProcessWorkOSDirectoryAttributesEventsParams, payload workosDirectoryGroupMembershipEventPayload) bool {
	return params.EntityType == WorkOSDirectoryAttributesEntityTypeGroupMembership &&
		params.WorkOSDirectoryGroupID == payload.Group.ID &&
		params.WorkOSDirectoryUserID == payload.User.ID
}

func directoryAttributesSyncEntityID(params ProcessWorkOSDirectoryAttributesEventsParams) (string, error) {
	switch params.EntityType {
	case WorkOSDirectoryAttributesEntityTypeGroup:
		if params.WorkOSDirectoryGroupID == "" {
			return "", oops.E(oops.CodeBadRequest, nil, "missing WorkOS directory group ID")
		}
		return params.WorkOSDirectoryGroupID, nil
	case WorkOSDirectoryAttributesEntityTypeGroupMembership:
		if params.WorkOSDirectoryGroupID == "" || params.WorkOSDirectoryUserID == "" {
			return "", oops.E(oops.CodeBadRequest, nil, "missing WorkOS directory group membership IDs")
		}
		return params.WorkOSDirectoryGroupID + ":" + params.WorkOSDirectoryUserID, nil
	default:
		return "", oops.E(oops.CodeBadRequest, nil, "unsupported directory attributes entity type %q", params.EntityType)
	}
}
