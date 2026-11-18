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

// ActionShadowMCPOfficialServerSet records an administrator choosing the
// official version of a Shadow MCP server, keeping direct connections to it
// working, or restoring the default. One verb covers all three: restoring
// the default is itself a choice, and it is the moment direct connections
// start being blocked.
const ActionShadowMCPOfficialServerSet Action = "shadow_mcp_official_server:set"

// ShadowMCPOfficialServerSnapshot is the choice before or after a change.
type ShadowMCPOfficialServerSnapshot struct {
	ServerURL string `json:"server_url"`
	// MCPServerID is empty when the server that proxies to ServerURL is
	// official.
	MCPServerID       string `json:"mcp_server_id,omitempty"`
	AllowDirectAccess bool   `json:"allow_direct_access"`
}

type LogShadowMCPOfficialServerSetEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	// ChoiceURN identifies the stored choice; for a restore to the default it
	// is the row that was removed.
	ChoiceURN urn.ShadowMCPOfficialServer
	ServerURL string

	SnapshotBefore *ShadowMCPOfficialServerSnapshot
	SnapshotAfter  *ShadowMCPOfficialServerSnapshot
}

func (l *Logger) LogShadowMCPOfficialServerSet(ctx context.Context, dbtx repo.DBTX, event LogShadowMCPOfficialServerSetEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.SnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionShadowMCPOfficialServerSet, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.SnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionShadowMCPOfficialServerSet, err)
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

			Action: string(ActionShadowMCPOfficialServerSet),

			SubjectID:          event.ChoiceURN.ID.String(),
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
