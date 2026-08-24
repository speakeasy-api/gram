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

const (
	// ActionPlatformMcpDiagnosticsUserStatusRead records an exact user-MCP
	// diagnosis. Every other Platform MCP diagnostic answers about a project or
	// a server; this one answers about a person, so it is audited on its own
	// rather than left inside the aggregate reads.
	ActionPlatformMcpDiagnosticsUserStatusRead Action = "platform-mcp-diagnostics:user_status_read"
)

// LogPlatformMcpDiagnosticsUserStatusReadEvent records that an administrator
// asked about one subject's status against one MCP.
//
// The subject is the MCP, not the person: an audit trail that named the person
// would republish the identity the diagnostic itself masks, and would make the
// audit log a place to enumerate an organization's people. What is recorded is
// the masked identity the caller was shown, which is enough to reconcile an
// entry against the answer it describes and not enough to learn anyone.
type LogPlatformMcpDiagnosticsUserStatusReadEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor urn.Principal

	McpServerURN urn.McpServer
	// MaskedIdentity is the same masked value returned to the caller. It is
	// never the raw identifier.
	MaskedIdentity string
	// Window is the resolved observation window the answer covered.
	Window string
}

func (l *Logger) LogPlatformMcpDiagnosticsUserStatusRead(ctx context.Context, dbtx repo.DBTX, event LogPlatformMcpDiagnosticsUserStatusReadEvent) error {
	action := ActionPlatformMcpDiagnosticsUserStatusRead
	metadata, err := marshalAuditPayload(map[string]string{
		"masked_identity": event.MaskedIdentity,
		"window":          event.Window,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(nil),
		ActorSlug:        conv.PtrToPGTextEmpty(nil),

		Action: string(action),

		SubjectID:          event.McpServerURN.ID.String(),
		SubjectType:        string(subjectTypeMcpServer),
		SubjectDisplayName: conv.PtrToPGTextEmpty(nil),
		SubjectSlug:        conv.PtrToPGTextEmpty(nil),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.PlatformMcpDiagnosticsV1})
}
