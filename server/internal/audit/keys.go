package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionKeyCreate       Action = "api_key:create"
	ActionKeyRevoke       Action = "api_key:revoke"
	ActionKeyPolicyUpdate Action = "api_key:update_policy"
)

type AgentKeyCredentialMetadata struct {
	SubjectURN             string          `json:"subject_urn"`
	DelegatedGrants        json.RawMessage `json:"delegated_grants"`
	DelegatedGrantsVersion int32           `json:"delegated_grants_version"`
	ExpiresAt              string          `json:"expires_at"`
}

type LogKeyCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.APIKey
	KeyName string

	Scopes          []string
	AgentCredential *AgentKeyCredentialMetadata
	// PolicyGrants is an ordinary key's own policy, written with the key.
	PolicyGrants json.RawMessage
}

func keyAuditMetadata(scopes []string, credential *AgentKeyCredentialMetadata, policyGrants json.RawMessage) ([]byte, error) {
	fields := map[string]any{"scopes": scopes}
	if credential != nil {
		fields["agent_credential"] = credential
	}
	if policyGrants != nil {
		fields["policy_grants"] = policyGrants
	}
	return marshalAuditPayload(fields)
}

func (l *Logger) LogKeyCreate(ctx context.Context, dbtx repo.DBTX, event LogKeyCreateEvent) error {
	action := ActionKeyCreate

	metadata, err := keyAuditMetadata(event.Scopes, event.AgentCredential, event.PolicyGrants)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeAPIKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.APIKeyV1})
}

type LogKeyRevokeEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.APIKey
	KeyName string

	Scopes          []string
	AgentCredential *AgentKeyCredentialMetadata
	// PolicyGrants is the policy an ordinary key held when it was revoked.
	PolicyGrants json.RawMessage
}

func (l *Logger) LogKeyRevoke(ctx context.Context, dbtx repo.DBTX, event LogKeyRevokeEvent) error {
	action := ActionKeyRevoke

	metadata, err := keyAuditMetadata(event.Scopes, event.AgentCredential, event.PolicyGrants)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeAPIKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.APIKeyV1})
}

type LogKeyPolicyUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.APIKey
	KeyName string

	PolicyGrantsBefore json.RawMessage
	PolicyGrantsAfter  json.RawMessage
}

func (l *Logger) LogKeyPolicyUpdate(ctx context.Context, dbtx repo.DBTX, event LogKeyPolicyUpdateEvent) error {
	action := ActionKeyPolicyUpdate

	// Both sides are whole policies, so an empty one is recorded as [].
	emptyPolicy := json.RawMessage("[]")
	if len(event.PolicyGrantsBefore) == 0 {
		event.PolicyGrantsBefore = emptyPolicy
	}
	if len(event.PolicyGrantsAfter) == 0 {
		event.PolicyGrantsAfter = emptyPolicy
	}

	beforeSnapshot, err := marshalAuditPayload(map[string]any{"policy_grants": event.PolicyGrantsBefore})
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(map[string]any{"policy_grants": event.PolicyGrantsAfter})
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeAPIKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.APIKeyV1})
}
