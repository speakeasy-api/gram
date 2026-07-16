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
	ActionTunneledMcpServerHeaderCreate Action = "tunneled-mcp-server-header:create"
	ActionTunneledMcpServerHeaderUpdate Action = "tunneled-mcp-server-header:update"
	ActionTunneledMcpServerHeaderDelete Action = "tunneled-mcp-server-header:delete"
)

// tunneledMcpServerHeaderMetadata carries the parent server on every header
// event so a reader can attribute a header to its server without a second
// lookup.
func tunneledMcpServerHeaderMetadata(serverURN urn.TunneledMcpServer, serverName string) ([]byte, error) {
	metadata, err := marshalAuditPayload(map[string]any{
		"tunneled_mcp_server":      serverURN.String(),
		"tunneled_mcp_server_name": serverName,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal tunneled mcp server header metadata: %w", err)
	}

	return metadata, nil
}

type LogTunneledMcpServerHeaderCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TunneledMcpServerHeaderURN  urn.TunneledMcpServerHeader
	TunneledMcpServerHeaderName string
	TunneledMcpServerURN        urn.TunneledMcpServer
	TunneledMcpServerName       string
}

func (l *Logger) LogTunneledMcpServerHeaderCreate(ctx context.Context, dbtx repo.DBTX, event LogTunneledMcpServerHeaderCreateEvent) error {
	action := ActionTunneledMcpServerHeaderCreate

	metadata, err := tunneledMcpServerHeaderMetadata(event.TunneledMcpServerURN, event.TunneledMcpServerName)
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

		SubjectID:          event.TunneledMcpServerHeaderURN.ID.String(),
		SubjectType:        string(subjectTypeTunneledMcpServerHeader),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TunneledMcpServerHeaderName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TunneledMcpServerHeaderV1})
}

type LogTunneledMcpServerHeaderUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TunneledMcpServerHeaderURN            urn.TunneledMcpServerHeader
	TunneledMcpServerHeaderName           string
	TunneledMcpServerURN                  urn.TunneledMcpServer
	TunneledMcpServerName                 string
	TunneledMcpServerHeaderSnapshotBefore *types.TunneledMcpServerHeader
	TunneledMcpServerHeaderSnapshotAfter  *types.TunneledMcpServerHeader
}

// LogTunneledMcpServerHeaderUpdate records a header mutation. Snapshots must be
// built from redacted header rows — a secret header's plaintext value must
// never reach the audit log or the outbox payload.
func (l *Logger) LogTunneledMcpServerHeaderUpdate(ctx context.Context, dbtx repo.DBTX, event LogTunneledMcpServerHeaderUpdateEvent) error {
	action := ActionTunneledMcpServerHeaderUpdate

	beforeSnapshot, err := marshalAuditPayload(event.TunneledMcpServerHeaderSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.TunneledMcpServerHeaderSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	metadata, err := tunneledMcpServerHeaderMetadata(event.TunneledMcpServerURN, event.TunneledMcpServerName)
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

		SubjectID:          event.TunneledMcpServerHeaderURN.ID.String(),
		SubjectType:        string(subjectTypeTunneledMcpServerHeader),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TunneledMcpServerHeaderName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TunneledMcpServerHeaderV1})
}

type LogTunneledMcpServerHeaderDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TunneledMcpServerHeaderURN  urn.TunneledMcpServerHeader
	TunneledMcpServerHeaderName string
	TunneledMcpServerURN        urn.TunneledMcpServer
	TunneledMcpServerName       string
}

// LogTunneledMcpServerHeaderDelete records the removal of an individual header.
// Headers soft-deleted as part of a cascading tunneled MCP server delete do not
// emit this event: that cascade is covered by the parent's tunneled-mcp:delete
// entry, and a per-header event there would be noise. The event deliberately
// carries only the header's identity, never a snapshot: delete call sites read
// raw rows straight from the database, so snapshotting one would persist an
// encrypted secret value into the audit log and the outbox payload.
func (l *Logger) LogTunneledMcpServerHeaderDelete(ctx context.Context, dbtx repo.DBTX, event LogTunneledMcpServerHeaderDeleteEvent) error {
	action := ActionTunneledMcpServerHeaderDelete

	metadata, err := tunneledMcpServerHeaderMetadata(event.TunneledMcpServerURN, event.TunneledMcpServerName)
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

		SubjectID:          event.TunneledMcpServerHeaderURN.ID.String(),
		SubjectType:        string(subjectTypeTunneledMcpServerHeader),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TunneledMcpServerHeaderName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.TunneledMcpServerHeaderV1})
}
