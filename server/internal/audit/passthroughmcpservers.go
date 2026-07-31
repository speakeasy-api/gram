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
	ActionPassthroughMcpServerCreate Action = "passthrough-mcp:create"
	ActionPassthroughMcpServerDelete Action = "passthrough-mcp:delete"
)

type LogPassthroughMcpServerCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	PassthroughMcpServerURN urn.PassthroughMcpServer
	PassthroughMcpServerURL string
}

func (l *Logger) LogPassthroughMcpServerCreate(ctx context.Context, dbtx repo.DBTX, event LogPassthroughMcpServerCreateEvent) error {
	action := ActionPassthroughMcpServerCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.PassthroughMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypePassthroughMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.PassthroughMcpServerURL),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.PassthroughMcpServerV1})
}

type LogPassthroughMcpServerDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	PassthroughMcpServerURN urn.PassthroughMcpServer
	PassthroughMcpServerURL string
}

func (l *Logger) LogPassthroughMcpServerDelete(ctx context.Context, dbtx repo.DBTX, event LogPassthroughMcpServerDeleteEvent) error {
	action := ActionPassthroughMcpServerDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.PassthroughMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypePassthroughMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.PassthroughMcpServerURL),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.PassthroughMcpServerV1})
}
