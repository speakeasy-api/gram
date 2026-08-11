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
	ActionUnproxiedMcpServerCreate Action = "unproxied-mcp:create"
	ActionUnproxiedMcpServerDelete Action = "unproxied-mcp:delete"
)

type LogUnproxiedMcpServerCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UnproxiedMcpServerURN urn.UnproxiedMcpServer
	UnproxiedMcpServerURL string
}

func (l *Logger) LogUnproxiedMcpServerCreate(ctx context.Context, dbtx repo.DBTX, event LogUnproxiedMcpServerCreateEvent) error {
	action := ActionUnproxiedMcpServerCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UnproxiedMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeUnproxiedMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.UnproxiedMcpServerURL),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UnproxiedMcpServerV1})
}

type LogUnproxiedMcpServerDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UnproxiedMcpServerURN urn.UnproxiedMcpServer
	UnproxiedMcpServerURL string
}

func (l *Logger) LogUnproxiedMcpServerDelete(ctx context.Context, dbtx repo.DBTX, event LogUnproxiedMcpServerDeleteEvent) error {
	action := ActionUnproxiedMcpServerDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UnproxiedMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeUnproxiedMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.UnproxiedMcpServerURL),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UnproxiedMcpServerV1})
}
