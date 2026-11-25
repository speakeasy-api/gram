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
	ActionAgentPolicyGrantCreate Action = "agent:policy_grant_create"
	ActionAgentPolicyGrantUpdate Action = "agent:policy_grant_update"
	ActionAgentPolicyGrantDelete Action = "agent:policy_grant_delete"
	// A connection owner blocked an agent from, or unblocked it on, their own
	// connection.
	ActionAgentConnectionBlockCreate Action = "agent:connection_block_create"
	ActionAgentConnectionBlockDelete Action = "agent:connection_block_delete"
	// A person attached their own connection to an agent, or detached it.
	// Recorded alongside remote-session:attach and remote-session:detach,
	// whose subject is the connection, so the agent's own history shows it.
	ActionAgentConnectionAttach Action = "agent:connection_attach"
	ActionAgentConnectionDetach Action = "agent:connection_detach"
	ActionAgentOwnerLoss        Action = "agent:owner_loss"
	ActionAgentTransfer         Action = "agent:transfer"
	ActionAgentReassign         Action = "agent:reassign"
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
		IsInternal:         false,
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
		IsInternal:         false,
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

// AgentConnectionMetadata names the connection a person attached to an agent
// or detached from it.
type AgentConnectionMetadata struct {
	BindingID             uuid.UUID `json:"binding_id"`
	RemoteSessionID       uuid.UUID `json:"remote_session_id"`
	RemoteSessionClientID uuid.UUID `json:"remote_session_client_id"`
	UserSessionIssuerID   uuid.UUID `json:"user_session_issuer_id"`
	// UpstreamAccount is the upstream account the connection signs in as,
	// when the connection reports one.
	UpstreamAccount string `json:"upstream_account,omitempty"`
}

type LogAgentConnectionEvent struct {
	OrganizationID   string
	ProjectID        uuid.UUID
	AgentURN         urn.Identity
	Actor            urn.Principal
	ActorDisplayName *string
	Action           Action
	Name             string
	Metadata         AgentConnectionMetadata
}

func (l *Logger) LogAgentConnection(ctx context.Context, dbtx repo.DBTX, event LogAgentConnectionEvent) error {
	metadata, err := marshalAuditPayload(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", event.Action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID:     event.OrganizationID,
		ProjectID:          uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},
		ActorID:            event.Actor.ID,
		ActorType:          string(event.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:          conv.ToPGTextEmpty(""),
		Action:             string(event.Action),
		SubjectID:          event.AgentURN.ID,
		SubjectType:        string(subjectTypeAgent),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),
		BeforeSnapshot:     nil,
		AfterSnapshot:      nil,
		Metadata:           metadata,
		ActingSurface:      conv.ToPGTextEmpty(""),
		ActingClientID:     conv.ToPGTextEmpty(""),
		IsInternal:         false,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AgentV1})
}
