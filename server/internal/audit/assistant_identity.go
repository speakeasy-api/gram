package audit

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AssistantExecutionAttribution separates authority and credential ownership.
// Tool attempts leave credential owner empty: only successful credential
// resolution can assert which user supplied the credential. Never include tokens.
type AssistantExecutionAttribution struct {
	AgentID               uuid.UUID `json:"agent_id"`
	TriggerID             uuid.UUID `json:"trigger_id"`
	WorkloadIssuerID      uuid.UUID `json:"workload_issuer_id"`
	WorkloadSubject       string    `json:"workload_subject"`
	EventID               string    `json:"event_id"`
	InitiatingUserID      string    `json:"initiating_user_id,omitempty"`
	CredentialOwnerUserID string    `json:"credential_owner_user_id,omitempty"`
}

const ActionAssistantCredentialUse Action = "assistant:credential_use"

func (l *Logger) LogAssistantCredentialUse(ctx context.Context, db repo.DBTX, org string, project, assistant uuid.UUID, identity AssistantExecutionAttribution) error {
	metadata, err := marshalAuditPayload(identity)
	if err != nil {
		return fmt.Errorf("marshal assistant credential attribution: %w", err)
	}
	return l.log(ctx, db, auditEntry{Params: repo.InsertAuditLogParams{
		OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, ActorID: identity.AgentID.String(), ActorType: string(urn.PrincipalTypeAgent),
		Action: string(ActionAssistantCredentialUse), SubjectID: assistant.String(), SubjectType: string(subjectTypeAssistant), Metadata: metadata,
		ActorDisplayName: pgtype.Text{String: "", Valid: false}, ActorSlug: pgtype.Text{String: "", Valid: false}, SubjectDisplayName: pgtype.Text{String: "", Valid: false}, SubjectSlug: pgtype.Text{String: "", Valid: false}, BeforeSnapshot: nil, AfterSnapshot: nil, ActingSurface: pgtype.Text{String: "", Valid: false}, ActingClientID: pgtype.Text{String: "", Valid: false},
	}, OutboxEvent: events.AssistantCredentialUseV1})
}
