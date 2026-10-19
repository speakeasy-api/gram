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
	ActionSigintSensorCreate Action = "sigint-sensor:create"
	ActionSigintSensorUpdate Action = "sigint-sensor:update"
	ActionSigintSensorDelete Action = "sigint-sensor:delete"
)

type LogSigintSensorCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SensorURN  urn.SigintSensor
	SensorName string
}

func (l *Logger) LogSigintSensorCreate(ctx context.Context, dbtx repo.DBTX, event LogSigintSensorCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionSigintSensorCreate),

		SubjectID:          event.SensorURN.ID.String(),
		SubjectType:        string(subjectTypeSigintSensor),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SensorName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SigintSensorV1})
}

type LogSigintSensorUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SensorURN            urn.SigintSensor
	SensorName           string
	SensorSnapshotBefore *types.SigintSensor
	SensorSnapshotAfter  *types.SigintSensor
}

func (l *Logger) LogSigintSensorUpdate(ctx context.Context, dbtx repo.DBTX, event LogSigintSensorUpdateEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.SensorSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionSigintSensorUpdate, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.SensorSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionSigintSensorUpdate, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionSigintSensorUpdate),

		SubjectID:          event.SensorURN.ID.String(),
		SubjectType:        string(subjectTypeSigintSensor),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SensorName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SigintSensorV1})
}

type LogSigintSensorDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SensorURN  urn.SigintSensor
	SensorName string
}

func (l *Logger) LogSigintSensorDelete(ctx context.Context, dbtx repo.DBTX, event LogSigintSensorDeleteEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionSigintSensorDelete),

		SubjectID:          event.SensorURN.ID.String(),
		SubjectType:        string(subjectTypeSigintSensor),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SensorName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SigintSensorV1})
}
