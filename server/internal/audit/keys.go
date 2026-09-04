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
	ActionKeyCreate Action = "api_key:create"
	ActionKeyRevoke Action = "api_key:revoke"
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
}

func keyAuditMetadata(scopes []string, credential *AgentKeyCredentialMetadata) ([]byte, error) {
	fields := map[string]any{"scopes": scopes}
	if credential != nil {
		fields["agent_credential"] = credential
	}
	return marshalAuditPayload(fields)
}

func (l *Logger) LogKeyCreate(ctx context.Context, dbtx repo.DBTX, event LogKeyCreateEvent) error {
	action := ActionKeyCreate

	metadata, err := keyAuditMetadata(event.Scopes, event.AgentCredential)
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
}

func (l *Logger) LogKeyRevoke(ctx context.Context, dbtx repo.DBTX, event LogKeyRevokeEvent) error {
	action := ActionKeyRevoke

	metadata, err := keyAuditMetadata(event.Scopes, event.AgentCredential)
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
