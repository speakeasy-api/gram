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
	ActionRemoteSessionClientCreate Action = "remote-session-client:create"
	ActionRemoteSessionClientUpdate Action = "remote-session-client:update"
	ActionRemoteSessionClientDelete Action = "remote-session-client:delete"
)

type LogRemoteSessionClientCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteSessionClientURN urn.RemoteSessionClient
	ClientID               string //nolint:glint // RFC 7591 client_id (issuer-assigned opaque string), distinct from the resource's URN/UUID.
}

func (l *Logger) LogRemoteSessionClientCreate(ctx context.Context, dbtx repo.DBTX, event LogRemoteSessionClientCreateEvent) error {
	action := ActionRemoteSessionClientCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RemoteSessionClientURN.ID.String(),
		SubjectType:        string(subjectTypeRemoteSessionClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ClientID),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RemoteSessionClientV1})
}

type LogRemoteSessionClientUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteSessionClientURN urn.RemoteSessionClient
	ClientID               string //nolint:glint // RFC 7591 client_id (issuer-assigned opaque string), distinct from the resource's URN/UUID.

	SnapshotBefore *types.RemoteSessionClient
	SnapshotAfter  *types.RemoteSessionClient
}

func (l *Logger) LogRemoteSessionClientUpdate(ctx context.Context, dbtx repo.DBTX, event LogRemoteSessionClientUpdateEvent) error {
	action := ActionRemoteSessionClientUpdate

	beforeSnapshot, err := marshalAuditPayload(event.SnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.SnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RemoteSessionClientURN.ID.String(),
		SubjectType:        string(subjectTypeRemoteSessionClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ClientID),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RemoteSessionClientV1})
}

type LogRemoteSessionClientDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteSessionClientURN urn.RemoteSessionClient
	ClientID               string //nolint:glint // RFC 7591 client_id (issuer-assigned opaque string), distinct from the resource's URN/UUID.
}

func (l *Logger) LogRemoteSessionClientDelete(ctx context.Context, dbtx repo.DBTX, event LogRemoteSessionClientDeleteEvent) error {
	action := ActionRemoteSessionClientDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RemoteSessionClientURN.ID.String(),
		SubjectType:        string(subjectTypeRemoteSessionClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ClientID),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RemoteSessionClientV1})
}
