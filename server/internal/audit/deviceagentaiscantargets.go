package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionAiScanTargetCreate Action = "device_agent_ai_scan_target:create"
	ActionAiScanTargetUpdate Action = "device_agent_ai_scan_target:update"
	ActionAiScanTargetDelete Action = "device_agent_ai_scan_target:delete"
)

type LogAiScanTargetCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AiScanTargetURN   urn.AiScanTarget
	TargetDisplayName string

	AiScanTargetSnapshotAfter *aitargets.Target
}

type LogAiScanTargetUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AiScanTargetURN   urn.AiScanTarget
	TargetDisplayName string

	AiScanTargetSnapshotBefore *aitargets.Target
	AiScanTargetSnapshotAfter  *aitargets.Target
}

type LogAiScanTargetDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AiScanTargetURN   urn.AiScanTarget
	TargetDisplayName string

	AiScanTargetSnapshotBefore *aitargets.Target
}

func (l *Logger) LogAiScanTargetCreate(ctx context.Context, dbtx repo.DBTX, event LogAiScanTargetCreateEvent) error {
	afterSnapshot, err := marshalAuditPayload(event.AiScanTargetSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionAiScanTargetCreate, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      aiScanTargetAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionAiScanTargetCreate, event.AiScanTargetURN, event.TargetDisplayName, nil, afterSnapshot),
		OutboxEvent: events.AiScanTargetV1,
	})
}

func (l *Logger) LogAiScanTargetUpdate(ctx context.Context, dbtx repo.DBTX, event LogAiScanTargetUpdateEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.AiScanTargetSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionAiScanTargetUpdate, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.AiScanTargetSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionAiScanTargetUpdate, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      aiScanTargetAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionAiScanTargetUpdate, event.AiScanTargetURN, event.TargetDisplayName, beforeSnapshot, afterSnapshot),
		OutboxEvent: events.AiScanTargetV1,
	})
}

func (l *Logger) LogAiScanTargetDelete(ctx context.Context, dbtx repo.DBTX, event LogAiScanTargetDeleteEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.AiScanTargetSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionAiScanTargetDelete, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      aiScanTargetAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionAiScanTargetDelete, event.AiScanTargetURN, event.TargetDisplayName, beforeSnapshot, nil),
		OutboxEvent: events.AiScanTargetV1,
	})
}

func aiScanTargetAuditParams(
	organizationID string,
	actor urn.Principal,
	actorDisplayName *string,
	actorSlug *string,
	action Action,
	target urn.AiScanTarget,
	targetDisplayName string,
	beforeSnapshot []byte,
	afterSnapshot []byte,
) repo.InsertAuditLogParams {
	return repo.InsertAuditLogParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          actor.ID,
		ActorType:        string(actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(actorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(actorSlug),

		Action: string(action),

		SubjectID:          target.ID,
		SubjectType:        string(subjectTypeAiScanTarget),
		SubjectDisplayName: conv.ToPGTextEmpty(targetDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}
}
