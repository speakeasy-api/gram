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
	ActionMcpEndpointCreate Action = "mcp-endpoint:create"
	ActionMcpEndpointUpdate Action = "mcp-endpoint:update"
	ActionMcpEndpointDelete Action = "mcp-endpoint:delete"
)

type LogMcpEndpointCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpEndpointURN urn.McpEndpoint
	Slug           string
}

func (l *Logger) LogMcpEndpointCreate(ctx context.Context, dbtx repo.DBTX, event LogMcpEndpointCreateEvent) error {
	action := ActionMcpEndpointCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.McpEndpointURN.ID.String(),
		SubjectType:        string(subjectTypeMcpEndpoint),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogMcpEndpointUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpEndpointURN            urn.McpEndpoint
	Slug                      string
	McpEndpointSnapshotBefore *types.McpEndpoint
	McpEndpointSnapshotAfter  *types.McpEndpoint
}

func (l *Logger) LogMcpEndpointUpdate(ctx context.Context, dbtx repo.DBTX, event LogMcpEndpointUpdateEvent) error {
	action := ActionMcpEndpointUpdate

	beforeSnapshot, err := marshalAuditPayload(event.McpEndpointSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.McpEndpointSnapshotAfter)
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

		SubjectID:          event.McpEndpointURN.ID.String(),
		SubjectType:        string(subjectTypeMcpEndpoint),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogMcpEndpointDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpEndpointURN urn.McpEndpoint
	Slug           string
}

func (l *Logger) LogMcpEndpointDelete(ctx context.Context, dbtx repo.DBTX, event LogMcpEndpointDeleteEvent) error {
	action := ActionMcpEndpointDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.McpEndpointURN.ID.String(),
		SubjectType:        string(subjectTypeMcpEndpoint),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
