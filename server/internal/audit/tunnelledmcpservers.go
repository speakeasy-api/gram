package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionTunnelledMcpServerCreate Action = "tunnelled-mcp:create"
	ActionTunnelledMcpServerUpdate Action = "tunnelled-mcp:update"
	ActionTunnelledMcpServerDelete Action = "tunnelled-mcp:delete"
)

type LogTunnelledMcpServerCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TunnelledMcpServerURN  urn.TunnelledMcpServer
	TunnelledMcpServerName string
}

func (l *Logger) LogTunnelledMcpServerCreate(ctx context.Context, dbtx repo.DBTX, event LogTunnelledMcpServerCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionTunnelledMcpServerCreate),

		SubjectID:          event.TunnelledMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeTunnelledMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TunnelledMcpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TunnelledMcpServerV1})
}

type LogTunnelledMcpServerUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TunnelledMcpServerURN            urn.TunnelledMcpServer
	TunnelledMcpServerName           string
	TunnelledMcpServerSnapshotBefore *types.TunnelledMcpServer
	TunnelledMcpServerSnapshotAfter  *types.TunnelledMcpServer
}

func (l *Logger) LogTunnelledMcpServerUpdate(ctx context.Context, dbtx repo.DBTX, event LogTunnelledMcpServerUpdateEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.TunnelledMcpServerSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionTunnelledMcpServerUpdate, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.TunnelledMcpServerSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionTunnelledMcpServerUpdate, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionTunnelledMcpServerUpdate),

		SubjectID:          event.TunnelledMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeTunnelledMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TunnelledMcpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TunnelledMcpServerV1})
}

type LogTunnelledMcpServerDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TunnelledMcpServerURN  urn.TunnelledMcpServer
	TunnelledMcpServerName string
}

func (l *Logger) LogTunnelledMcpServerDelete(ctx context.Context, dbtx repo.DBTX, event LogTunnelledMcpServerDeleteEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionTunnelledMcpServerDelete),

		SubjectID:          event.TunnelledMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeTunnelledMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TunnelledMcpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TunnelledMcpServerV1})
}
