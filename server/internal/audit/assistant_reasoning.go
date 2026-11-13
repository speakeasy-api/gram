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

const ActionAssistantReasoningUpdate Action = "assistant:update_reasoning_effort"

// AssistantReasoningSnapshot excludes instructions and credentials: only the
// changed inference preference belongs in this configuration audit event.
type AssistantReasoningSnapshot struct {
	ReasoningEffort string `json:"reasoning_effort"`
}

type LogAssistantReasoningUpdateEvent struct {
	OrganizationID                   string
	ProjectID                        uuid.UUID
	Actor                            urn.Principal
	ActorDisplayName                 *string
	AssistantURN                     urn.Assistant
	AssistantName                    string
	AssistantReasoningSnapshotBefore *AssistantReasoningSnapshot
	AssistantReasoningSnapshotAfter  *AssistantReasoningSnapshot
}

func (l *Logger) LogAssistantReasoningUpdate(ctx context.Context, dbtx repo.DBTX, event LogAssistantReasoningUpdateEvent) error {
	before, err := marshalAuditPayload(event.AssistantReasoningSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal assistant reasoning before snapshot: %w", err)
	}
	after, err := marshalAuditPayload(event.AssistantReasoningSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal assistant reasoning after snapshot: %w", err)
	}
	entry := repo.InsertAuditLogParams{
		IsInternal: false,
		ActorSlug:  conv.ToPGTextEmpty(""), SubjectSlug: conv.ToPGTextEmpty(""),
		Metadata: nil, ActingSurface: conv.ToPGTextEmpty(""), ActingClientID: conv.ToPGTextEmpty(""),
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: true},
		ActorID:        event.Actor.ID, ActorType: string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		Action:           string(ActionAssistantReasoningUpdate),
		SubjectID:        event.AssistantURN.ID.String(), SubjectType: string(subjectTypeAssistant),
		SubjectDisplayName: conv.ToPGTextEmpty(event.AssistantName),
		BeforeSnapshot:     before, AfterSnapshot: after,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AssistantConfigurationV1})
}
