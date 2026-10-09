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

	// ActionMcpServerScopePinUpdate records a change to the scope pin on a remote-backed server's protected resource.
	ActionMcpServerScopePinUpdate Action = "mcp-server:update-scope-pin"

	// ActionMcpServerEnvironmentLink records an environment becoming linked to
	// an MCP server, either newly or in place of another environment.
	ActionMcpServerEnvironmentLink Action = "mcp-server:link-environment"
	// ActionMcpServerEnvironmentUnlink records an MCP server's environment link
	// being removed.
	ActionMcpServerEnvironmentUnlink Action = "mcp-server:unlink-environment"
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

type LogMcpServerScopePinUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpServerURN  urn.McpServer
	McpServerName string
	McpServerSlug string
	ResourceURL   string
	// AffectedMcpServerIDs lists every live server sharing the resource, the subject included.
	AffectedMcpServerIDs []string
	ScopesBefore         []string
	ScopesAfter          []string
}

// LogMcpServerScopePinUpdate records a change to the scope pin on the
// protected resource a remote-backed MCP server's logins are for.
func (l *Logger) LogMcpServerScopePinUpdate(ctx context.Context, dbtx repo.DBTX, event LogMcpServerScopePinUpdateEvent) error {
	action := ActionMcpServerScopePinUpdate

	beforeSnapshot, err := marshalAuditPayload(map[string]any{"pinned_scopes": conv.DefaultSlice(event.ScopesBefore, []string{})})
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(map[string]any{"pinned_scopes": conv.DefaultSlice(event.ScopesAfter, []string{})})
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	metadata, err := marshalAuditPayload(map[string]any{
		"resource_url":   event.ResourceURL,
		"mcp_server_ids": conv.DefaultSlice(event.AffectedMcpServerIDs, []string{}),
	})
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

		SubjectID:          event.McpServerURN.ID.String(),
		SubjectType:        string(subjectTypeMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.McpServerName),
		SubjectSlug:        conv.ToPGTextEmpty(event.McpServerSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.McpServerV1})
}

type LogMcpServerEnvironmentLinkEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpServerURN  urn.McpServer
	McpServerName string
	McpServerSlug string

	// EnvironmentURN is the environment now linked to the server.
	EnvironmentURN urn.Environment
	// PreviousEnvironmentURN is the environment it replaced, nil when the
	// server had none.
	PreviousEnvironmentURN *urn.Environment
}

// LogMcpServerEnvironmentLink records an environment being linked to an MCP
// server. Linking hands the environment's values to whatever the server
// fronts, so the event names both the environment and any one it replaced.
func (l *Logger) LogMcpServerEnvironmentLink(ctx context.Context, dbtx repo.DBTX, event LogMcpServerEnvironmentLinkEvent) error {
	action := ActionMcpServerEnvironmentLink

	fields := map[string]any{"environment_id": event.EnvironmentURN.ID.String()}
	if event.PreviousEnvironmentURN != nil {
		fields["previous_environment_id"] = event.PreviousEnvironmentURN.ID.String()
	}
	metadata, err := marshalAuditPayload(fields)
	if err != nil {
		return fmt.Errorf("build %s metadata: %w", action, err)
	}

	return l.log(ctx, dbtx, auditEntry{Params: mcpServerEnvironmentEntry(action, event.OrganizationID, event.ProjectID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.McpServerURN, event.McpServerName, event.McpServerSlug, metadata), OutboxEvent: events.McpServerV1})
}

type LogMcpServerEnvironmentUnlinkEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpServerURN  urn.McpServer
	McpServerName string
	McpServerSlug string

	// EnvironmentURN is the environment that was unlinked.
	EnvironmentURN urn.Environment
}

// LogMcpServerEnvironmentUnlink records an MCP server's environment link being
// removed.
func (l *Logger) LogMcpServerEnvironmentUnlink(ctx context.Context, dbtx repo.DBTX, event LogMcpServerEnvironmentUnlinkEvent) error {
	action := ActionMcpServerEnvironmentUnlink

	metadata, err := marshalAuditPayload(map[string]any{"environment_id": event.EnvironmentURN.ID.String()})
	if err != nil {
		return fmt.Errorf("build %s metadata: %w", action, err)
	}

	return l.log(ctx, dbtx, auditEntry{Params: mcpServerEnvironmentEntry(action, event.OrganizationID, event.ProjectID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.McpServerURN, event.McpServerName, event.McpServerSlug, metadata), OutboxEvent: events.McpServerV1})
}

func mcpServerEnvironmentEntry(action Action, organizationID string, projectID uuid.UUID, actor urn.Principal, actorDisplayName, actorSlug *string, server urn.McpServer, name, slug string, metadata []byte) repo.InsertAuditLogParams {
	return repo.InsertAuditLogParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: projectID != uuid.Nil},

		ActorID:          actor.ID,
		ActorType:        string(actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(actorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(actorSlug),

		Action: string(action),

		SubjectID:          server.ID.String(),
		SubjectType:        string(subjectTypeMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(name),
		SubjectSlug:        conv.ToPGTextEmpty(slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}
}
