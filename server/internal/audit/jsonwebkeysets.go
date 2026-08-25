package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionJsonWebKeySetCreate Action = "json_web_key_set:create"
	ActionJsonWebKeySetUpdate Action = "json_web_key_set:update"
	ActionJsonWebKeySetDelete Action = "json_web_key_set:delete"
)

// JsonWebKeySetSnapshot is the audited state of a JSON Web Key Set. The set
// carries configuration only — its published keys are audited under their own
// subject — so the snapshot is the name and the backing external key.
type JsonWebKeySetSnapshot struct {
	Name          string `json:"name"`
	ExternalKeyID string `json:"external_key_id"`
}

type LogJsonWebKeySetCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SetURN  urn.JsonWebKeySet
	SetName string
}

func (l *Logger) LogJsonWebKeySetCreate(ctx context.Context, dbtx repo.DBTX, event LogJsonWebKeySetCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionJsonWebKeySetCreate),

		SubjectID:          event.SetURN.ID.String(),
		SubjectType:        string(subjectTypeJsonWebKeySet),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SetName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.JsonWebKeySetV1})
}

type LogJsonWebKeySetUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SetURN  urn.JsonWebKeySet
	SetName string

	SetSnapshotBefore *JsonWebKeySetSnapshot
	SetSnapshotAfter  *JsonWebKeySetSnapshot
}

func (l *Logger) LogJsonWebKeySetUpdate(ctx context.Context, dbtx repo.DBTX, event LogJsonWebKeySetUpdateEvent) error {
	action := ActionJsonWebKeySetUpdate

	before, err := marshalAuditPayload(event.SetSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	after, err := marshalAuditPayload(event.SetSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.SetURN.ID.String(),
		SubjectType:        string(subjectTypeJsonWebKeySet),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SetName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.JsonWebKeySetV1})
}

type LogJsonWebKeySetDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SetURN  urn.JsonWebKeySet
	SetName string
}

func (l *Logger) LogJsonWebKeySetDelete(ctx context.Context, dbtx repo.DBTX, event LogJsonWebKeySetDeleteEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionJsonWebKeySetDelete),

		SubjectID:          event.SetURN.ID.String(),
		SubjectType:        string(subjectTypeJsonWebKeySet),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SetName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.JsonWebKeySetV1})
}
