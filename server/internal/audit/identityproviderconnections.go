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
	ActionIdentityProviderConnectionCreate         Action = "identity-provider-connection:create"
	ActionIdentityProviderConnectionSubmitClientID Action = "identity-provider-connection:submit-client-id"
	ActionIdentityProviderConnectionVerify         Action = "identity-provider-connection:verify"
	ActionIdentityProviderConnectionRecordAgent    Action = "identity-provider-connection:record-agent"
	ActionIdentityProviderConnectionRevoke         Action = "identity-provider-connection:revoke"
)

// IdentityProviderConnectionSnapshot is the connection state an audit entry
// records. It carries no key material, tokens, or raw provider responses.
type IdentityProviderConnectionSnapshot struct {
	Provider      string   `json:"provider"`
	Status        string   `json:"status"`
	OrgURL        string   `json:"org_url"`
	ListingMode   string   `json:"listing_mode"`
	ClientID      string   `json:"client_id,omitempty"`
	DPoPRequired  bool     `json:"dpop_required"`
	GrantedScopes []string `json:"granted_scopes"`
	Reasons       []string `json:"verification_reasons,omitempty"`
	LastError     string   `json:"last_error,omitempty"`
	AgentID       string   `json:"agent_id,omitempty"`
	AgentAppID    string   `json:"agent_app_id,omitempty"`
}

type LogIdentityProviderConnectionEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConnectionURN urn.IdentityProviderConnection
	Provider      string

	SnapshotBefore *IdentityProviderConnectionSnapshot
	SnapshotAfter  *IdentityProviderConnectionSnapshot
}

func (l *Logger) LogIdentityProviderConnectionCreate(ctx context.Context, dbtx repo.DBTX, event LogIdentityProviderConnectionEvent) error {
	return l.logIdentityProviderConnection(ctx, dbtx, ActionIdentityProviderConnectionCreate, event)
}

func (l *Logger) LogIdentityProviderConnectionSubmitClientID(ctx context.Context, dbtx repo.DBTX, event LogIdentityProviderConnectionEvent) error {
	return l.logIdentityProviderConnection(ctx, dbtx, ActionIdentityProviderConnectionSubmitClientID, event)
}

func (l *Logger) LogIdentityProviderConnectionVerify(ctx context.Context, dbtx repo.DBTX, event LogIdentityProviderConnectionEvent) error {
	return l.logIdentityProviderConnection(ctx, dbtx, ActionIdentityProviderConnectionVerify, event)
}

func (l *Logger) LogIdentityProviderConnectionRecordAgent(ctx context.Context, dbtx repo.DBTX, event LogIdentityProviderConnectionEvent) error {
	return l.logIdentityProviderConnection(ctx, dbtx, ActionIdentityProviderConnectionRecordAgent, event)
}

func (l *Logger) LogIdentityProviderConnectionRevoke(ctx context.Context, dbtx repo.DBTX, event LogIdentityProviderConnectionEvent) error {
	return l.logIdentityProviderConnection(ctx, dbtx, ActionIdentityProviderConnectionRevoke, event)
}

func (l *Logger) logIdentityProviderConnection(ctx context.Context, dbtx repo.DBTX, action Action, event LogIdentityProviderConnectionEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.SnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.SnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConnectionURN.ID.String(),
		SubjectType:        string(subjectTypeIdentityProviderConnection),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Provider),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.IdentityProviderConnectionV1})
}
