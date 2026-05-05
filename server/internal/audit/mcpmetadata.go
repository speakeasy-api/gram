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
	ActionMCPMetadataUpdate Action = "mcp_metadata:update"
)

type LogMCPMetadataUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ToolsetURN  urn.Toolset
	ToolsetName string
	ToolsetSlug string

	MCPMetadataSnapshotBefore *types.McpMetadata
	MCPMetadataSnapshotAfter  *types.McpMetadata
}

func (l *Logger) LogMCPMetadataUpdate(ctx context.Context, dbtx repo.DBTX, event LogMCPMetadataUpdateEvent) error {
	action := ActionMCPMetadataUpdate

	var beforePayload any
	if event.MCPMetadataSnapshotBefore != nil {
		beforePayload = event.MCPMetadataSnapshotBefore
	}
	beforeSnapshot, err := marshalAuditPayload(beforePayload)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.MCPMetadataSnapshotAfter)
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

		SubjectID:          event.ToolsetURN.ID.String(),
		SubjectType:        string(subjectTypeToolset),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ToolsetName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ToolsetSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
