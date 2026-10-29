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
	ActionAuthClientCreate          Action = "auth-client:create"
	ActionAuthClientUpdate          Action = "auth-client:update"
	ActionAuthClientUpdateHeaders   Action = "auth-client:update-headers"
	ActionAuthClientEnable          Action = "auth-client:enable"
	ActionAuthClientDisable         Action = "auth-client:disable"
	ActionAuthClientAttachMcpServer Action = "auth-client:attach-mcp-server"
	ActionAuthClientDetachMcpServer Action = "auth-client:detach-mcp-server"
	ActionAuthClientAudienceUpdate  Action = "auth-client:update-audience"
)

type LogAuthClientUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AuthClientURN  urn.AuthClient
	AuthClientName string

	AuthClientSnapshotBefore *types.AuthClient
	AuthClientSnapshotAfter  *types.AuthClient
}

// LogAuthClientUpdate records a rename. Enabling and disabling are recorded
// with their own actions so they can be found without diffing snapshots.
func (l *Logger) LogAuthClientUpdate(ctx context.Context, dbtx repo.DBTX, event LogAuthClientUpdateEvent) error {
	return l.logAuthClientChange(ctx, dbtx, ActionAuthClientUpdate, event)
}

// LogAuthClientCreate records a new client. Only the after snapshot is set,
// and it never carries header values.
func (l *Logger) LogAuthClientCreate(ctx context.Context, dbtx repo.DBTX, event LogAuthClientUpdateEvent) error {
	return l.logAuthClientChange(ctx, dbtx, ActionAuthClientCreate, event)
}

// LogAuthClientUpdateHeaders records a change to the headers a client sends,
// including a rotated secret. Snapshots never carry header values.
func (l *Logger) LogAuthClientUpdateHeaders(ctx context.Context, dbtx repo.DBTX, event LogAuthClientUpdateEvent) error {
	return l.logAuthClientChange(ctx, dbtx, ActionAuthClientUpdateHeaders, event)
}

func (l *Logger) LogAuthClientEnable(ctx context.Context, dbtx repo.DBTX, event LogAuthClientUpdateEvent) error {
	return l.logAuthClientChange(ctx, dbtx, ActionAuthClientEnable, event)
}

func (l *Logger) LogAuthClientDisable(ctx context.Context, dbtx repo.DBTX, event LogAuthClientUpdateEvent) error {
	return l.logAuthClientChange(ctx, dbtx, ActionAuthClientDisable, event)
}

func (l *Logger) logAuthClientChange(ctx context.Context, dbtx repo.DBTX, action Action, event LogAuthClientUpdateEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.AuthClientSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.AuthClientSnapshotAfter)
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

		SubjectID:          event.AuthClientURN.ID.String(),
		SubjectType:        string(subjectTypeAuthClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.AuthClientName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AuthClientV1})
}

type LogAuthClientMcpServerEvent struct {
	OrganizationID string
	// ProjectID is the server's project.
	ProjectID uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AuthClientURN  urn.AuthClient
	AuthClientName string
	McpServerURN   urn.McpServer
	McpServerName  string
}

func (l *Logger) LogAuthClientDetachMcpServer(ctx context.Context, dbtx repo.DBTX, event LogAuthClientMcpServerEvent) error {
	return l.logAuthClientServerChange(ctx, dbtx, ActionAuthClientDetachMcpServer, event)
}

// LogAuthClientAttachMcpServer records a server starting to offer the client.
func (l *Logger) LogAuthClientAttachMcpServer(ctx context.Context, dbtx repo.DBTX, event LogAuthClientMcpServerEvent) error {
	return l.logAuthClientServerChange(ctx, dbtx, ActionAuthClientAttachMcpServer, event)
}

func (l *Logger) logAuthClientServerChange(ctx context.Context, dbtx repo.DBTX, action Action, event LogAuthClientMcpServerEvent) error {
	metadata, err := marshalAuditPayload(map[string]any{"mcp_server_id": event.McpServerURN.ID.String(), "mcp_server_name": event.McpServerName})
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

		SubjectID:          event.AuthClientURN.ID.String(),
		SubjectType:        string(subjectTypeAuthClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.AuthClientName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AuthClientV1})
}

type LogAuthClientAudienceUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AuthClientURN  urn.AuthClient
	AuthClientName string

	AudienceSnapshotBefore *types.AuthClientAudience
	AudienceSnapshotAfter  *types.AuthClientAudience
}

// LogAuthClientAudienceUpdate records a change to who may use an auth client.
func (l *Logger) LogAuthClientAudienceUpdate(ctx context.Context, dbtx repo.DBTX, event LogAuthClientAudienceUpdateEvent) error {
	action := ActionAuthClientAudienceUpdate

	beforeSnapshot, err := marshalAuditPayload(event.AudienceSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.AudienceSnapshotAfter)
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

		SubjectID:          event.AuthClientURN.ID.String(),
		SubjectType:        string(subjectTypeAuthClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.AuthClientName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AuthClientV1})
}
