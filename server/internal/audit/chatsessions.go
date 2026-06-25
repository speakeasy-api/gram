package audit

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionChatSessionAccess Action = "chat_session:access"
)

type LogChatSessionAccessEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ChatSessionURN urn.ChatSession
	ChatTitle      string
	// OwnerUserID is the user id of the session owner, recorded so reviewers can
	// see whose session was accessed even when the accessor differs.
	OwnerUserID string
}

// LogChatSessionAccess records that a chat session transcript was read. Unlike
// most audit events this describes a read, not a mutation, so callers pass the
// pool directly as dbtx — there is no surrounding transaction to be atomic with.
func (l *Logger) LogChatSessionAccess(ctx context.Context, dbtx repo.DBTX, event LogChatSessionAccessEvent) error {
	action := ActionChatSessionAccess

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ChatSessionURN.ID.String(),
		SubjectType:        string(subjectTypeChatSession),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ChatTitle),
		SubjectSlug:        conv.ToPGTextEmpty(event.OwnerUserID),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ChatSessionV1})
}
