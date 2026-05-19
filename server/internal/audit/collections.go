package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionMcpCollectionCreate       Action = "mcp_collection:create"
	ActionMcpCollectionUpdate       Action = "mcp_collection:update"
	ActionMcpCollectionDelete       Action = "mcp_collection:delete"
	ActionMcpCollectionAttachServer Action = "mcp_collection:attach_server"
	ActionMcpCollectionDetachServer Action = "mcp_collection:detach_server"
)

type LogMcpCollectionCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CollectionURN  urn.McpCollection
	CollectionName string
	CollectionSlug string
}

func (l *Logger) LogMcpCollectionCreate(ctx context.Context, dbtx repo.DBTX, event LogMcpCollectionCreateEvent) error {
	action := ActionMcpCollectionCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.CollectionURN.ID.String(),
		SubjectType:        string(subjectTypeMcpCollection),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CollectionName),
		SubjectSlug:        conv.ToPGTextEmpty(event.CollectionSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, entry)
}

type LogMcpCollectionUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CollectionURN            urn.McpCollection
	CollectionName           string
	CollectionSlug           string
	CollectionSnapshotBefore *types.MCPCollection
	CollectionSnapshotAfter  *types.MCPCollection
}

func (l *Logger) LogMcpCollectionUpdate(ctx context.Context, dbtx repo.DBTX, event LogMcpCollectionUpdateEvent) error {
	action := ActionMcpCollectionUpdate

	beforeSnapshot, err := marshalAuditPayload(event.CollectionSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.CollectionSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.CollectionURN.ID.String(),
		SubjectType:        string(subjectTypeMcpCollection),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CollectionName),
		SubjectSlug:        conv.ToPGTextEmpty(event.CollectionSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, entry)
}

type LogMcpCollectionDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CollectionURN  urn.McpCollection
	CollectionName string
	CollectionSlug string
}

func (l *Logger) LogMcpCollectionDelete(ctx context.Context, dbtx repo.DBTX, event LogMcpCollectionDeleteEvent) error {
	action := ActionMcpCollectionDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.CollectionURN.ID.String(),
		SubjectType:        string(subjectTypeMcpCollection),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CollectionName),
		SubjectSlug:        conv.ToPGTextEmpty(event.CollectionSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, entry)
}

type LogMcpCollectionAttachServerEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CollectionURN  urn.McpCollection
	CollectionName string
	CollectionSlug string
	ToolsetURN     urn.Toolset
}

func (l *Logger) LogMcpCollectionAttachServer(ctx context.Context, dbtx repo.DBTX, event LogMcpCollectionAttachServerEvent) error {
	action := ActionMcpCollectionAttachServer

	metadata, err := marshalAuditPayload(map[string]string{
		"toolset_id": event.ToolsetURN.ID.String(),
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.CollectionURN.ID.String(),
		SubjectType:        string(subjectTypeMcpCollection),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CollectionName),
		SubjectSlug:        conv.ToPGTextEmpty(event.CollectionSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, entry)
}

type LogMcpCollectionDetachServerEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CollectionURN  urn.McpCollection
	CollectionName string
	CollectionSlug string
	ToolsetURN     urn.Toolset
}

func (l *Logger) LogMcpCollectionDetachServer(ctx context.Context, dbtx repo.DBTX, event LogMcpCollectionDetachServerEvent) error {
	action := ActionMcpCollectionDetachServer

	metadata, err := marshalAuditPayload(map[string]string{
		"toolset_id": event.ToolsetURN.ID.String(),
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.CollectionURN.ID.String(),
		SubjectType:        string(subjectTypeMcpCollection),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CollectionName),
		SubjectSlug:        conv.ToPGTextEmpty(event.CollectionSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, entry)
}
