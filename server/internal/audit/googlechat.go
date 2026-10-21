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

// LogGoogleChat records a safe connection, binding, or mapping change atomically.
func (l *Logger) LogGoogleChat(ctx context.Context, tx repo.DBTX, org string, project uuid.NullUUID, actor urn.Principal, subject interface{ String() string }, action string, after any) error {
	var id uuid.UUID
	var kind string
	switch value := subject.(type) {
	case urn.GoogleChatConnection:
		id = value.ID
		kind = "google_chat_connection"
	case urn.GoogleChatBinding:
		id = value.ID
		kind = "google_chat_binding"
	case urn.GoogleChatMember:
		id = value.ID
		kind = "google_chat_member"
	default:
		return fmt.Errorf("unsupported Google Chat audit subject")
	}
	snapshot, err := marshalAuditPayload(after)
	if err != nil {
		return fmt.Errorf("encode Google Chat audit: %w", err)
	}
	return l.log(ctx, tx, auditEntry{Params: repo.InsertAuditLogParams{OrganizationID: org, ProjectID: project, ActorID: actor.ID, ActorType: string(actor.Type), ActorDisplayName: conv.ToPGTextEmpty(""), ActorSlug: conv.ToPGTextEmpty(""), Action: "google-chat:" + action, SubjectID: id.String(), SubjectType: kind, SubjectDisplayName: conv.ToPGTextEmpty(""), SubjectSlug: conv.ToPGTextEmpty(""), BeforeSnapshot: nil, AfterSnapshot: snapshot, Metadata: nil}, OutboxEvent: events.GoogleChatV1})
}
