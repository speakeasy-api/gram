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
	ActionAgentCreate  Action = "agent:create"
	ActionAgentRename  Action = "agent:rename"
	ActionAgentSuspend Action = "agent:suspend"
	ActionAgentResume  Action = "agent:resume"
	ActionAgentRevoke  Action = "agent:revoke"
	ActionAgentDelete  Action = "agent:delete"
)

// AgentSnapshot is the bounded, organization-visible audit projection of an
// agent. It intentionally excludes policy and credential data.
type AgentSnapshot struct {
	OwnerUserID string `json:"owner_user_id"`
	Name        string `json:"name"`
	Lifecycle   string `json:"lifecycle"`
}

type LogAgentEvent struct {
	OrganizationID   string
	AgentID          uuid.UUID
	Actor            urn.Principal
	ActorDisplayName *string
	Action           Action
	Name             string
	Before           *AgentSnapshot
	After            *AgentSnapshot
}

func (l *Logger) LogAgent(ctx context.Context, dbtx repo.DBTX, event LogAgentEvent) error {
	before, err := marshalAuditPayload(event.Before)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", event.Action, err)
	}
	after, err := marshalAuditPayload(event.After)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", event.Action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID:     event.OrganizationID,
		ActorID:            event.Actor.ID,
		ActorType:          string(event.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(event.ActorDisplayName),
		Action:             string(event.Action),
		SubjectID:          event.AgentID.String(),
		SubjectType:        string(subjectTypeAgent),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		BeforeSnapshot:     before,
		AfterSnapshot:      after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AgentV1})
}
