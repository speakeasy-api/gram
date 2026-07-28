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
	ActionTunneledMcpServerCreate    Action = "tunneled-mcp:create"
	ActionTunneledMcpServerUpdate    Action = "tunneled-mcp:update"
	ActionTunneledMcpServerRotateKey Action = "tunneled-mcp:rotate-key"
	ActionTunneledMcpServerDelete    Action = "tunneled-mcp:delete"
)

type LogTunneledMcpServerCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TunneledMcpServerURN  urn.TunneledMcpServer
	TunneledMcpServerName string
}

func (l *Logger) LogTunneledMcpServerCreate(ctx context.Context, dbtx repo.DBTX, event LogTunneledMcpServerCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionTunneledMcpServerCreate),

		SubjectID:          event.TunneledMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeTunneledMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TunneledMcpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TunneledMcpServerV1})
}

type LogTunneledMcpServerUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TunneledMcpServerURN            urn.TunneledMcpServer
	TunneledMcpServerName           string
	TunneledMcpServerSnapshotBefore *types.TunneledMcpServer
	TunneledMcpServerSnapshotAfter  *types.TunneledMcpServer
}

func (l *Logger) LogTunneledMcpServerUpdate(ctx context.Context, dbtx repo.DBTX, event LogTunneledMcpServerUpdateEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.TunneledMcpServerSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionTunneledMcpServerUpdate, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.TunneledMcpServerSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionTunneledMcpServerUpdate, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionTunneledMcpServerUpdate),

		SubjectID:          event.TunneledMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeTunneledMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TunneledMcpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TunneledMcpServerV1})
}

type LogTunneledMcpServerRotateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TunneledMcpServerURN            urn.TunneledMcpServer
	TunneledMcpServerName           string
	TunneledMcpServerSnapshotBefore *types.TunneledMcpServer
	TunneledMcpServerSnapshotAfter  *types.TunneledMcpServer
}

func (l *Logger) LogTunneledMcpServerRotate(ctx context.Context, dbtx repo.DBTX, event LogTunneledMcpServerRotateEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.TunneledMcpServerSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionTunneledMcpServerRotateKey, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.TunneledMcpServerSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionTunneledMcpServerRotateKey, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionTunneledMcpServerRotateKey),

		SubjectID:          event.TunneledMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeTunneledMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TunneledMcpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TunneledMcpServerV1})
}

type LogTunneledMcpServerDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TunneledMcpServerURN  urn.TunneledMcpServer
	TunneledMcpServerName string
}

func (l *Logger) LogTunneledMcpServerDelete(ctx context.Context, dbtx repo.DBTX, event LogTunneledMcpServerDeleteEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionTunneledMcpServerDelete),

		SubjectID:          event.TunneledMcpServerURN.ID.String(),
		SubjectType:        string(subjectTypeTunneledMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TunneledMcpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TunneledMcpServerV1})
}
