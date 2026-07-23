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
	ActionMcpServerCreate Action = "mcp-server:create"
	ActionMcpServerUpdate Action = "mcp-server:update"
	ActionMcpServerDelete Action = "mcp-server:delete"

	// ActionMcpServerToolMetadataUpdate is recorded once per mutation of an MCP
	// server's tool metadata. The subject is the server itself: a batch write
	// that touches fifty tools produces one entry carrying before and after
	// snapshots of the whole collection, rather than fifty per-row entries.
	ActionMcpServerToolMetadataUpdate Action = "mcp-server:update-tool-metadata"
)

type LogMcpServerCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpServerURN  urn.McpServer
	McpServerName string
	McpServerSlug string
}

func (l *Logger) LogMcpServerCreate(ctx context.Context, dbtx repo.DBTX, event LogMcpServerCreateEvent) error {
	action := ActionMcpServerCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.McpServerURN.ID.String(),
		SubjectType:        string(subjectTypeMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.McpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(event.McpServerSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.McpServerV1})
}

type LogMcpServerUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpServerURN            urn.McpServer
	McpServerName           string
	McpServerSlug           string
	McpServerSnapshotBefore *types.McpServer
	McpServerSnapshotAfter  *types.McpServer
}

func (l *Logger) LogMcpServerUpdate(ctx context.Context, dbtx repo.DBTX, event LogMcpServerUpdateEvent) error {
	action := ActionMcpServerUpdate

	beforeSnapshot, err := marshalAuditPayload(event.McpServerSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.McpServerSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.McpServerURN.ID.String(),
		SubjectType:        string(subjectTypeMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.McpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(event.McpServerSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.McpServerV1})
}

type LogMcpServerToolMetadataUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpServerURN               urn.McpServer
	McpServerName              string
	McpServerSlug              string
	ToolMetadataSnapshotBefore []*types.ToolMetadata
	ToolMetadataSnapshotAfter  []*types.ToolMetadata
}

func (l *Logger) LogMcpServerToolMetadataUpdate(ctx context.Context, dbtx repo.DBTX, event LogMcpServerToolMetadataUpdateEvent) error {
	action := ActionMcpServerToolMetadataUpdate

	beforeSnapshot, err := marshalAuditPayload(event.ToolMetadataSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.ToolMetadataSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.McpServerURN.ID.String(),
		SubjectType:        string(subjectTypeMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.McpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(event.McpServerSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.McpServerV1})
}

type LogMcpServerDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpServerURN  urn.McpServer
	McpServerName string
	McpServerSlug string
}

func (l *Logger) LogMcpServerDelete(ctx context.Context, dbtx repo.DBTX, event LogMcpServerDeleteEvent) error {
	action := ActionMcpServerDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.McpServerURN.ID.String(),
		SubjectType:        string(subjectTypeMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.McpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(event.McpServerSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.McpServerV1})
}
