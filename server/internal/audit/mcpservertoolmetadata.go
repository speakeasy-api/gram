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

// ActionMcpServerToolMetadataUpdate is recorded once per mutation of an MCP
// server's tool metadata. The subject is the server's metadata collection, not
// an individual tool row: a batch write that touches fifty tools produces one
// entry carrying before and after snapshots of the whole collection, rather
// than fifty per-row entries.
const ActionMcpServerToolMetadataUpdate Action = "mcp-server-tool-metadata:update"

type LogMcpServerToolMetadataUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	// McpServerToolMetadataURN identifies the collection, so it is built from
	// the MCP server's id.
	McpServerToolMetadataURN urn.McpServerToolMetadata
	McpServerName            string
	McpServerSlug            string
	SnapshotBefore           []*types.ToolMetadata
	SnapshotAfter            []*types.ToolMetadata
}

func (l *Logger) LogMcpServerToolMetadataUpdate(ctx context.Context, dbtx repo.DBTX, event LogMcpServerToolMetadataUpdateEvent) error {
	action := ActionMcpServerToolMetadataUpdate

	beforeSnapshot, err := marshalAuditPayload(event.SnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.SnapshotAfter)
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

		SubjectID:          event.McpServerToolMetadataURN.ID.String(),
		SubjectType:        string(subjectTypeMcpServerToolMetadata),
		SubjectDisplayName: conv.ToPGTextEmpty(event.McpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(event.McpServerSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.McpServerToolMetadataV1})
}
