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

const (
	ActionRemoteSessionAttach Action = "remote-session:attach"
	ActionRemoteSessionDetach Action = "remote-session:detach"
)

// RemoteSessionBindingMetadata identifies the exact authorization decision,
// without copying tokens or upstream account identity into the audit log.
type RemoteSessionBindingMetadata struct {
	BindingID             uuid.UUID `json:"binding_id"`
	PrincipalID           uuid.UUID `json:"principal_id"`
	UserSessionIssuerID   uuid.UUID `json:"user_session_issuer_id"`
	RemoteSessionClientID uuid.UUID `json:"remote_session_client_id"`
	GrantGeneration       int64     `json:"grant_generation"`
}

type LogRemoteSessionBindingEvent struct {
	OrganizationID   string
	ProjectID        uuid.UUID
	Actor            urn.Principal
	RemoteSessionURN urn.RemoteSession
	Metadata         RemoteSessionBindingMetadata
}

func (l *Logger) LogRemoteSessionBinding(ctx context.Context, dbtx repo.DBTX, action Action, event LogRemoteSessionBindingEvent) error {
	if action != ActionRemoteSessionAttach && action != ActionRemoteSessionDetach {
		return fmt.Errorf("invalid remote session binding action: %s", action)
	}
	metadata, err := marshalAuditPayload(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal binding audit metadata: %w", err)
	}
	return l.log(ctx, dbtx, auditEntry{Params: repo.InsertAuditLogParams{
		OrganizationID:   event.OrganizationID,
		ProjectID:        uuid.NullUUID{UUID: event.ProjectID, Valid: true},
		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		Action:           string(action),
		SubjectID:        event.RemoteSessionURN.ID.String(),
		SubjectType:      string(subjectTypeRemoteSession),
		Metadata:         metadata,
		ActorDisplayName: pgtype.Text{String: "", Valid: false}, ActorSlug: pgtype.Text{String: "", Valid: false}, SubjectDisplayName: pgtype.Text{String: "", Valid: false}, SubjectSlug: pgtype.Text{String: "", Valid: false}, BeforeSnapshot: nil, AfterSnapshot: nil, ActingSurface: pgtype.Text{String: "", Valid: false}, ActingClientID: pgtype.Text{String: "", Valid: false},
	}, OutboxEvent: events.RemoteSessionV1})
}
