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
	ActionAgentCreate            Action = "agent:create"
	ActionAgentRename            Action = "agent:rename"
	ActionAgentSuspend           Action = "agent:suspend"
	ActionAgentResume            Action = "agent:resume"
	ActionAgentRevoke            Action = "agent:revoke"
	ActionAgentDelete            Action = "agent:delete"
	ActionAgentOwnerLoss         Action = "agent:owner_loss"
	ActionAgentTransfer          Action = "agent:transfer"
	ActionAgentReassign          Action = "agent:reassign"
	ActionAgentPolicyGrantCreate Action = "agent:policy_grant_create"
	ActionAgentPolicyGrantUpdate Action = "agent:policy_grant_update"
	ActionAgentPolicyGrantDelete Action = "agent:policy_grant_delete"
)

// AgentSnapshot is the bounded, organization-visible audit projection of an
// agent. It intentionally excludes policy and credential data.
type AgentSnapshot struct {
	OwnerUserID                 string  `json:"owner_user_id"`
	OwnerReassignmentRequiredAt *string `json:"owner_reassignment_required_at,omitempty"`
	OwnerReassignmentReason     *string `json:"owner_reassignment_reason,omitempty"`
	Name                        string  `json:"name"`
	Lifecycle                   string  `json:"lifecycle"`
}

type LogAgentEvent struct {
	OrganizationID   string
	AgentURN         urn.Identity
	Actor            urn.Principal
	ActorDisplayName *string
	Action           Action
	Name             string
	Before           *AgentSnapshot
	After            *AgentSnapshot
}

// AgentPolicyGrantSnapshot is the bounded audit projection of one direct grant.
type AgentPolicyGrantSnapshot struct {
	ID       uuid.UUID         `json:"id"`
	Scope    string            `json:"scope"`
	Effect   string            `json:"effect"`
	Selector map[string]string `json:"selector"`
}

type LogAgentPolicyGrantEvent struct {
	OrganizationID   string
	AgentURN         urn.Identity
	Actor            urn.Principal
	ActorDisplayName *string
	Action           Action
	Name             string
	Before           *AgentPolicyGrantSnapshot
	After            *AgentPolicyGrantSnapshot
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
		ProjectID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID:            event.Actor.ID,
		ActorType:          string(event.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:          conv.ToPGTextEmpty(""),
		Action:             string(event.Action),
		SubjectID:          event.AgentURN.ID,
		SubjectType:        string(subjectTypeAgent),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),
		BeforeSnapshot:     before,
		AfterSnapshot:      after,
		Metadata:           nil,
		ActingSurface:      conv.ToPGTextEmpty(""),
		ActingClientID:     conv.ToPGTextEmpty(""),
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AgentV1})
}

func (l *Logger) LogAgentPolicyGrant(ctx context.Context, dbtx repo.DBTX, event LogAgentPolicyGrantEvent) error {
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
		ProjectID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID:            event.Actor.ID,
		ActorType:          string(event.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:          conv.ToPGTextEmpty(""),
		Action:             string(event.Action),
		SubjectID:          event.AgentURN.ID,
		SubjectType:        string(subjectTypeAgent),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),
		BeforeSnapshot:     before,
		AfterSnapshot:      after,
		Metadata:           nil,
		ActingSurface:      conv.ToPGTextEmpty(""),
		ActingClientID:     conv.ToPGTextEmpty(""),
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AgentV1})
}
