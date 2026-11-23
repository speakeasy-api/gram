package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A Shadow MCP server is linked automatically to the project MCP server that
// proxies to its URL. A stored link is an administrator's override: it names
// another MCP server, or keeps the Shadow MCP server in direct use. While a
// server is linked, direct connections to the Shadow MCP server are blocked
// wherever a Shadow MCP policy applies.
const (
	// ActionShadowMCPOfficialServerCreate records an administrator
	// overriding the automatic link, including adding back an override that
	// was removed.
	ActionShadowMCPOfficialServerCreate Action = "shadow_mcp_official_server:create"
	// ActionShadowMCPOfficialServerUpdate records an administrator changing
	// an override: another MCP server, or keeping direct use.
	ActionShadowMCPOfficialServerUpdate Action = "shadow_mcp_official_server:update"
	// ActionShadowMCPOfficialServerDelete records an administrator removing
	// an override, which brings back the automatic link.
	ActionShadowMCPOfficialServerDelete Action = "shadow_mcp_official_server:delete"
	// ActionShadowMCPOfficialServerSet is kept so earlier entries still
	// read: one verb used to cover every change to a link, including the
	// on/off switch links no longer have. Nothing writes it now.
	ActionShadowMCPOfficialServerSet Action = "shadow_mcp_official_server:set"
)

// ShadowMCPOfficialServerSnapshot is a link before or after a change.
type ShadowMCPOfficialServerSnapshot struct {
	ServerURL   string `json:"server_url"`
	MCPServerID string `json:"mcp_server_id"`
	// KeepShadow is true when the override keeps the Shadow MCP server in
	// direct use; MCPServerID is then empty.
	KeepShadow bool `json:"keep_shadow"`
}

// ShadowMCPOfficialServerEvent identifies who changed which link.
type ShadowMCPOfficialServerEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	// LinkURN identifies the stored link row.
	LinkURN   urn.ShadowMCPOfficialServer
	ServerURL string
}

type LogShadowMCPOfficialServerCreateEvent struct {
	ShadowMCPOfficialServerEvent
	ShadowMCPOfficialServerSnapshotAfter *ShadowMCPOfficialServerSnapshot
}

type LogShadowMCPOfficialServerUpdateEvent struct {
	ShadowMCPOfficialServerEvent
	ShadowMCPOfficialServerSnapshotBefore *ShadowMCPOfficialServerSnapshot
	ShadowMCPOfficialServerSnapshotAfter  *ShadowMCPOfficialServerSnapshot
}

type LogShadowMCPOfficialServerDeleteEvent struct {
	ShadowMCPOfficialServerEvent
	ShadowMCPOfficialServerSnapshotBefore *ShadowMCPOfficialServerSnapshot
}

func (l *Logger) LogShadowMCPOfficialServerCreate(ctx context.Context, dbtx repo.DBTX, event LogShadowMCPOfficialServerCreateEvent) error {
	return l.logShadowMCPOfficialServer(ctx, dbtx, ActionShadowMCPOfficialServerCreate, event.ShadowMCPOfficialServerEvent, nil, event.ShadowMCPOfficialServerSnapshotAfter)
}

func (l *Logger) LogShadowMCPOfficialServerUpdate(ctx context.Context, dbtx repo.DBTX, event LogShadowMCPOfficialServerUpdateEvent) error {
	return l.logShadowMCPOfficialServer(ctx, dbtx, ActionShadowMCPOfficialServerUpdate, event.ShadowMCPOfficialServerEvent, event.ShadowMCPOfficialServerSnapshotBefore, event.ShadowMCPOfficialServerSnapshotAfter)
}

func (l *Logger) LogShadowMCPOfficialServerDelete(ctx context.Context, dbtx repo.DBTX, event LogShadowMCPOfficialServerDeleteEvent) error {
	return l.logShadowMCPOfficialServer(ctx, dbtx, ActionShadowMCPOfficialServerDelete, event.ShadowMCPOfficialServerEvent, event.ShadowMCPOfficialServerSnapshotBefore, nil)
}

func (l *Logger) logShadowMCPOfficialServer(ctx context.Context, dbtx repo.DBTX, action Action, event ShadowMCPOfficialServerEvent, before, after *ShadowMCPOfficialServerSnapshot) error {
	beforeSnapshot, err := marshalAuditPayload(before)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(after)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params: repo.InsertAuditLogParams{
			IsInternal:     false,
			OrganizationID: event.OrganizationID,
			ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

			ActorID:          event.Actor.ID,
			ActorType:        string(event.Actor.Type),
			ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
			ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

			Action: string(action),

			SubjectID:          event.LinkURN.ID.String(),
			SubjectType:        string(subjectTypeShadowMCPOfficialServer),
			SubjectDisplayName: conv.ToPGTextEmpty(event.ServerURL),
			SubjectSlug:        conv.ToPGTextEmpty(""),

			Metadata:       nil,
			BeforeSnapshot: beforeSnapshot,
			AfterSnapshot:  afterSnapshot,
		},
		OutboxEvent: events.ShadowMCPOfficialServerV1,
	})
}
