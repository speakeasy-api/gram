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

const ActionAssistantExecutionProfileUpdate Action = "assistant:update_execution_profile"

// AssistantExecutionProfileChoice is one server's choice as an audit snapshot
// records it. AuthClientID is empty for no upstream authentication.
type AssistantExecutionProfileChoice struct {
	AuthClientID string `json:"auth_client_id,omitempty"`
	Source       string `json:"source"`
}

// LogAssistantExecutionProfileUpdateEvent records a change to the credential
// an assistant's turns use on one server, under one root trigger and
// execution context. A nil snapshot is no choice.
type LogAssistantExecutionProfileUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AssistantURN     urn.Assistant
	AssistantName    string
	TriggerURN       urn.TriggerInstance
	ExecutionContext string
	McpServerURN     urn.McpServer

	ChoiceBefore *AssistantExecutionProfileChoice
	ChoiceAfter  *AssistantExecutionProfileChoice
}

func (l *Logger) LogAssistantExecutionProfileUpdate(ctx context.Context, dbtx repo.DBTX, event LogAssistantExecutionProfileUpdateEvent) error {
	action := ActionAssistantExecutionProfileUpdate
	beforeSnapshot, err := marshalAuditPayload(event.ChoiceBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.ChoiceAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	metadata, err := marshalAuditPayload(map[string]any{
		"trigger_urn":       event.TriggerURN.String(),
		"execution_context": event.ExecutionContext,
		"mcp_server_urn":    event.McpServerURN.String(),
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.AssistantURN.ID.String(),
		SubjectType:        string(subjectTypeAssistant),
		SubjectDisplayName: conv.ToPGTextEmpty(event.AssistantName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AssistantExecutionProfileV1})
}
