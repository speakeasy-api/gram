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
	ActionRemoteMcpServerHeaderCreate Action = "remote-mcp-server-header:create"
	ActionRemoteMcpServerHeaderUpdate Action = "remote-mcp-server-header:update"
	ActionRemoteMcpServerHeaderDelete Action = "remote-mcp-server-header:delete"
)

// remoteMcpServerHeaderMetadata carries the parent server on every header event
// so a reader can attribute a header to its server without a second lookup.
func remoteMcpServerHeaderMetadata(serverURN urn.RemoteMcpServer, serverURL string) ([]byte, error) {
	metadata, err := marshalAuditPayload(map[string]any{
		"remote_mcp_server":     serverURN.String(),
		"remote_mcp_server_url": serverURL,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal remote mcp server header metadata: %w", err)
	}

	return metadata, nil
}

type LogRemoteMcpServerHeaderCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteMcpServerHeaderURN  urn.RemoteMcpServerHeader
	RemoteMcpServerHeaderName string
	RemoteMcpServerURN        urn.RemoteMcpServer
	RemoteMcpServerURL        string
}

func (l *Logger) LogRemoteMcpServerHeaderCreate(ctx context.Context, dbtx repo.DBTX, event LogRemoteMcpServerHeaderCreateEvent) error {
	action := ActionRemoteMcpServerHeaderCreate

	metadata, err := remoteMcpServerHeaderMetadata(event.RemoteMcpServerURN, event.RemoteMcpServerURL)
	if err != nil {
		return fmt.Errorf("build %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RemoteMcpServerHeaderURN.ID.String(),
		SubjectType:        string(subjectTypeRemoteMcpServerHeader),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RemoteMcpServerHeaderName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RemoteMcpServerHeaderV1})
}

type LogRemoteMcpServerHeaderUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteMcpServerHeaderURN            urn.RemoteMcpServerHeader
	RemoteMcpServerHeaderName           string
	RemoteMcpServerURN                  urn.RemoteMcpServer
	RemoteMcpServerURL                  string
	RemoteMcpServerHeaderSnapshotBefore *types.RemoteMcpServerHeader
	RemoteMcpServerHeaderSnapshotAfter  *types.RemoteMcpServerHeader
}

// LogRemoteMcpServerHeaderUpdate records a header mutation. Snapshots must be
// built from redacted header rows — a secret header's plaintext value must
// never reach the audit log or the outbox payload.
func (l *Logger) LogRemoteMcpServerHeaderUpdate(ctx context.Context, dbtx repo.DBTX, event LogRemoteMcpServerHeaderUpdateEvent) error {
	action := ActionRemoteMcpServerHeaderUpdate

	beforeSnapshot, err := marshalAuditPayload(event.RemoteMcpServerHeaderSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.RemoteMcpServerHeaderSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	metadata, err := remoteMcpServerHeaderMetadata(event.RemoteMcpServerURN, event.RemoteMcpServerURL)
	if err != nil {
		return fmt.Errorf("build %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RemoteMcpServerHeaderURN.ID.String(),
		SubjectType:        string(subjectTypeRemoteMcpServerHeader),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RemoteMcpServerHeaderName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RemoteMcpServerHeaderV1})
}

type LogRemoteMcpServerHeaderDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteMcpServerHeaderURN  urn.RemoteMcpServerHeader
	RemoteMcpServerHeaderName string
	RemoteMcpServerURN        urn.RemoteMcpServer
	RemoteMcpServerURL        string
}

// LogRemoteMcpServerHeaderDelete records the removal of an individual header.
// Headers soft-deleted as part of a cascading remote MCP server delete do not
// emit this event: that cascade is covered by the parent's remote-mcp:delete
// entry, and a per-header event there would be noise. The event deliberately
// carries only the header's identity, never a snapshot: delete call sites read
// raw rows straight from the database, so snapshotting one would persist an
// encrypted secret value into the audit log and the outbox payload.
func (l *Logger) LogRemoteMcpServerHeaderDelete(ctx context.Context, dbtx repo.DBTX, event LogRemoteMcpServerHeaderDeleteEvent) error {
	action := ActionRemoteMcpServerHeaderDelete

	metadata, err := remoteMcpServerHeaderMetadata(event.RemoteMcpServerURN, event.RemoteMcpServerURL)
	if err != nil {
		return fmt.Errorf("build %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RemoteMcpServerHeaderURN.ID.String(),
		SubjectType:        string(subjectTypeRemoteMcpServerHeader),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RemoteMcpServerHeaderName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RemoteMcpServerHeaderV1})
}
