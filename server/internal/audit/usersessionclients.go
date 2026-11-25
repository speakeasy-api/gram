package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionUserSessionClientCreate             Action = "user-session-client:create"
	ActionUserSessionClientUpdateRedirectURIs Action = "user-session-client:update-redirect-uris"
	ActionUserSessionClientRevoke             Action = "user-session-client:revoke"
	ActionUserSessionClientCIMDRefresh        Action = "user-session-client:cimd-refresh"
)

// LogUserSessionClientCreateEvent records an operator registering a client
// through the management API, as opposed to a client registering itself over
// DCR. The actor is the record of who registered it.
type LogUserSessionClientCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionClientURN           urn.UserSessionClient
	ClientID                       string //nolint:glint // auditeventurnnaming: RFC 7591 client_id minted by Speakeasy, distinct from the resource's URN/UUID.
	ClientName                     string
	UserSessionClientSnapshotAfter *types.UserSessionClient
}

func (l *Logger) LogUserSessionClientCreate(ctx context.Context, dbtx repo.DBTX, event LogUserSessionClientCreateEvent) error {
	action := ActionUserSessionClientCreate

	afterSnapshot, err := marshalAuditPayload(event.UserSessionClientSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		IsInternal:     false,
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UserSessionClientURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ClientName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ClientID),

		BeforeSnapshot: nil,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UserSessionClientV1})
}

// LogUserSessionClientUpdateRedirectURIsEvent records an operator replacing a
// client's redirect URIs. The snapshots carry the old and new lists.
type LogUserSessionClientUpdateRedirectURIsEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionClientURN            urn.UserSessionClient
	ClientID                        string //nolint:glint // auditeventurnnaming: RFC 7591 client_id, distinct from the resource's URN/UUID.
	ClientName                      string
	UserSessionClientSnapshotBefore *types.UserSessionClient
	UserSessionClientSnapshotAfter  *types.UserSessionClient
}

func (l *Logger) LogUserSessionClientUpdateRedirectURIs(ctx context.Context, dbtx repo.DBTX, event LogUserSessionClientUpdateRedirectURIsEvent) error {
	action := ActionUserSessionClientUpdateRedirectURIs

	beforeSnapshot, err := marshalAuditPayload(event.UserSessionClientSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.UserSessionClientSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		IsInternal:     false,
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UserSessionClientURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ClientName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ClientID),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UserSessionClientV1})
}

type LogUserSessionClientRevokeEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionClientURN urn.UserSessionClient
	ClientID             string //nolint:glint // auditeventurnnaming: RFC 7591 client_id (DCR-issued opaque string), distinct from the resource's URN/UUID.
	ClientName           string
}

func (l *Logger) LogUserSessionClientRevoke(ctx context.Context, dbtx repo.DBTX, event LogUserSessionClientRevokeEvent) error {
	action := ActionUserSessionClientRevoke
	entry := repo.InsertAuditLogParams{
		IsInternal:     false,
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UserSessionClientURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ClientName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ClientID),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UserSessionClientV1})
}

// LogUserSessionClientCIMDRefreshEvent records an operator forcing a CIMD
// client's metadata document to be re-read: the stored cache state is purged
// and the document re-fetched unconditionally. Emitted with the purge, so a
// refresh whose upstream fetch subsequently fails still leaves a record of
// the cache mutation.
type LogUserSessionClientCIMDRefreshEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionClientURN urn.UserSessionClient
	ClientID             string //nolint:glint // auditeventurnnaming: RFC 7591 client_id (for a CIMD client, the metadata document URL), distinct from the resource's URN/UUID.
	ClientName           string
}

func (l *Logger) LogUserSessionClientCIMDRefresh(ctx context.Context, dbtx repo.DBTX, event LogUserSessionClientCIMDRefreshEvent) error {
	action := ActionUserSessionClientCIMDRefresh
	entry := repo.InsertAuditLogParams{
		IsInternal:     false,
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UserSessionClientURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ClientName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ClientID),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UserSessionClientV1})
}
