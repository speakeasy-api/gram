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
	ActionSigintSignalCreate Action = "sigint-signal:create"
	ActionSigintSignalUpdate Action = "sigint-signal:update"
	ActionSigintSignalDelete Action = "sigint-signal:delete"
)

type LogSigintSignalCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SignalURN  urn.SigintSignal
	SignalName string
}

func (l *Logger) LogSigintSignalCreate(ctx context.Context, dbtx repo.DBTX, event LogSigintSignalCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionSigintSignalCreate),

		SubjectID:          event.SignalURN.ID.String(),
		SubjectType:        string(subjectTypeSigintSignal),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SignalName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SigintSignalV1})
}

type LogSigintSignalUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SignalURN            urn.SigintSignal
	SignalName           string
	SignalSnapshotBefore *types.SigintSignal
	SignalSnapshotAfter  *types.SigintSignal
}

func (l *Logger) LogSigintSignalUpdate(ctx context.Context, dbtx repo.DBTX, event LogSigintSignalUpdateEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.SignalSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionSigintSignalUpdate, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.SignalSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionSigintSignalUpdate, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionSigintSignalUpdate),

		SubjectID:          event.SignalURN.ID.String(),
		SubjectType:        string(subjectTypeSigintSignal),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SignalName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SigintSignalV1})
}

type LogSigintSignalDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SignalURN  urn.SigintSignal
	SignalName string
}

func (l *Logger) LogSigintSignalDelete(ctx context.Context, dbtx repo.DBTX, event LogSigintSignalDeleteEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionSigintSignalDelete),

		SubjectID:          event.SignalURN.ID.String(),
		SubjectType:        string(subjectTypeSigintSignal),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SignalName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SigintSignalV1})
}
