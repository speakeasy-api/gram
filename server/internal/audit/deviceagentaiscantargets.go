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
	ActionDeviceAgentAiScanTargetCreate Action = "device_agent_ai_scan_target:create"
	ActionDeviceAgentAiScanTargetUpdate Action = "device_agent_ai_scan_target:update"
	ActionDeviceAgentAiScanTargetDelete Action = "device_agent_ai_scan_target:delete"
)

type LogDeviceAgentAiScanTargetCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AiScanTargetURN   urn.DeviceAgentAiScanTarget
	TargetDisplayName string

	AiScanTargetSnapshotAfter *aitargets.Target
}

type LogDeviceAgentAiScanTargetUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AiScanTargetURN   urn.DeviceAgentAiScanTarget
	TargetDisplayName string

	AiScanTargetSnapshotBefore *aitargets.Target
	AiScanTargetSnapshotAfter  *aitargets.Target
}

type LogDeviceAgentAiScanTargetDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AiScanTargetURN   urn.DeviceAgentAiScanTarget
	TargetDisplayName string

	AiScanTargetSnapshotBefore *aitargets.Target
}

func (l *Logger) LogDeviceAgentAiScanTargetCreate(ctx context.Context, dbtx repo.DBTX, event LogDeviceAgentAiScanTargetCreateEvent) error {
	afterSnapshot, err := marshalAuditPayload(event.AiScanTargetSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionDeviceAgentAiScanTargetCreate, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      aiScanTargetAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionDeviceAgentAiScanTargetCreate, event.AiScanTargetURN, event.TargetDisplayName, nil, afterSnapshot),
		OutboxEvent: events.DeviceAgentAiScanTargetV1,
	})
}

func (l *Logger) LogDeviceAgentAiScanTargetUpdate(ctx context.Context, dbtx repo.DBTX, event LogDeviceAgentAiScanTargetUpdateEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.AiScanTargetSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionDeviceAgentAiScanTargetUpdate, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.AiScanTargetSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionDeviceAgentAiScanTargetUpdate, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      aiScanTargetAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionDeviceAgentAiScanTargetUpdate, event.AiScanTargetURN, event.TargetDisplayName, beforeSnapshot, afterSnapshot),
		OutboxEvent: events.DeviceAgentAiScanTargetV1,
	})
}

func (l *Logger) LogDeviceAgentAiScanTargetDelete(ctx context.Context, dbtx repo.DBTX, event LogDeviceAgentAiScanTargetDeleteEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.AiScanTargetSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionDeviceAgentAiScanTargetDelete, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      aiScanTargetAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionDeviceAgentAiScanTargetDelete, event.AiScanTargetURN, event.TargetDisplayName, beforeSnapshot, nil),
		OutboxEvent: events.DeviceAgentAiScanTargetV1,
	})
}

func aiScanTargetAuditParams(
	organizationID string,
	actor urn.Principal,
	actorDisplayName *string,
	actorSlug *string,
	action Action,
	target urn.DeviceAgentAiScanTarget,
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
		SubjectType:        string(subjectTypeDeviceAgentAiScanTarget),
		SubjectDisplayName: conv.ToPGTextEmpty(targetDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}
}
