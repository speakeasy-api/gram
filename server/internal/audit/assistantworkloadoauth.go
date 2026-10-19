package audit

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LogAssistantWorkloadOAuthEvent contains identifiers only, never credentials.
type LogAssistantWorkloadOAuthEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID
	Actor          urn.Principal
	Runtime        uuid.UUID
	ClientURN      urn.UserSessionClient
	AssistantURN   urn.Assistant
	TriggerURN     urn.TriggerInstance
	SessionURN     urn.UserSession // Zero means registration; nonzero means token issuance.
}

// LogAssistantWorkloadOAuth uses the caller's transaction for durable audit and outbox delivery.
func (l *Logger) LogAssistantWorkloadOAuth(ctx context.Context, tx repo.DBTX, e LogAssistantWorkloadOAuthEvent) error {
	action, subjectType, subjectID := "user-session-client:workload-register", "user-session-client", e.ClientURN.ID
	event := events.UserSessionClientV1
	if e.SessionURN.ID != uuid.Nil {
		action, subjectType, subjectID = "user-session:workload-issue", "user-session", e.SessionURN.ID
		event = events.UserSessionV1
	}
	metadata, err := json.Marshal(map[string]string{"runtime_id": e.Runtime.String(), "client_id": e.ClientURN.ID.String(), "assistant_id": e.AssistantURN.ID.String(), "trigger_id": e.TriggerURN.ID.String()})
	if err != nil {
		return err
	}
	return l.log(ctx, tx, auditEntry{Params: repo.InsertAuditLogParams{
		OrganizationID: e.OrganizationID, ProjectID: uuid.NullUUID{UUID: e.ProjectID, Valid: true},
		ActorID: e.Actor.ID, ActorType: string(e.Actor.Type), Action: action,
		SubjectID: subjectID.String(), SubjectType: subjectType, Metadata: metadata,
	}, OutboxEvent: event})
}
