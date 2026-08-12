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
	ActionLiteLLMInstanceCreate    Action = "litellm_instance:create"
	ActionLiteLLMInstanceRotateKey Action = "litellm_instance:rotate_key"
	ActionLiteLLMInstanceRevoke    Action = "litellm_instance:revoke"
)

type LogLiteLLMInstanceCreateEvent struct {
	OrganizationID   string
	ProjectID        uuid.UUID
	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string
	InstanceURN      urn.LiteLLMInstance
	InstanceName     string
}

type LiteLLMInstanceSnapshot struct {
	Name           string    `json:"name"`
	ProjectID      uuid.UUID `json:"project_id"`
	FailurePosture string    `json:"failure_posture"`
	KeyPrefix      string    `json:"key_prefix"`
	Active         bool      `json:"active"`
}

type LogLiteLLMInstanceRotateKeyEvent struct {
	OrganizationID   string
	ProjectID        uuid.UUID
	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string
	InstanceURN      urn.LiteLLMInstance
	InstanceName     string

	LiteLLMInstanceSnapshotBefore *LiteLLMInstanceSnapshot
	LiteLLMInstanceSnapshotAfter  *LiteLLMInstanceSnapshot
}

type LogLiteLLMInstanceRevokeEvent LogLiteLLMInstanceCreateEvent

func (l *Logger) LogLiteLLMInstanceCreate(ctx context.Context, dbtx repo.DBTX, event LogLiteLLMInstanceCreateEvent) error {
	return l.logLiteLLMInstance(ctx, dbtx, event, ActionLiteLLMInstanceCreate)
}

func (l *Logger) LogLiteLLMInstanceRotateKey(ctx context.Context, dbtx repo.DBTX, event LogLiteLLMInstanceRotateKeyEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.LiteLLMInstanceSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionLiteLLMInstanceRotateKey, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.LiteLLMInstanceSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionLiteLLMInstanceRotateKey, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID:     event.OrganizationID,
		ProjectID:          uuid.NullUUID{UUID: event.ProjectID, Valid: true},
		ActorID:            event.Actor.ID,
		ActorType:          string(event.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:          conv.PtrToPGTextEmpty(event.ActorSlug),
		Action:             string(ActionLiteLLMInstanceRotateKey),
		SubjectID:          event.InstanceURN.ID.String(),
		SubjectType:        string(subjectTypeLiteLLMInstance),
		SubjectDisplayName: conv.ToPGTextEmpty(event.InstanceName),
		SubjectSlug:        conv.ToPGTextEmpty(""),
		Metadata:           nil,
		BeforeSnapshot:     beforeSnapshot,
		AfterSnapshot:      afterSnapshot,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.LiteLLMInstanceV1})
}

func (l *Logger) LogLiteLLMInstanceRevoke(ctx context.Context, dbtx repo.DBTX, event LogLiteLLMInstanceRevokeEvent) error {
	return l.logLiteLLMInstance(ctx, dbtx, LogLiteLLMInstanceCreateEvent(event), ActionLiteLLMInstanceRevoke)
}

func (l *Logger) logLiteLLMInstance(ctx context.Context, dbtx repo.DBTX, event LogLiteLLMInstanceCreateEvent, action Action) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID:     event.OrganizationID,
		ProjectID:          uuid.NullUUID{UUID: event.ProjectID, Valid: true},
		ActorID:            event.Actor.ID,
		ActorType:          string(event.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:          conv.PtrToPGTextEmpty(event.ActorSlug),
		Action:             string(action),
		SubjectID:          event.InstanceURN.ID.String(),
		SubjectType:        string(subjectTypeLiteLLMInstance),
		SubjectDisplayName: conv.ToPGTextEmpty(event.InstanceName),
		SubjectSlug:        conv.ToPGTextEmpty(""),
		Metadata:           nil,
		BeforeSnapshot:     nil,
		AfterSnapshot:      nil,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.LiteLLMInstanceV1})
}
