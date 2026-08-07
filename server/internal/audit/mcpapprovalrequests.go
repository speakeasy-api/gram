package audit

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionMCPApprovalRequestCreate  Action = "mcp_approval_request:create"
	ActionMCPApprovalRequestApprove Action = "mcp_approval_request:approve"
	ActionMCPApprovalRequestDeny    Action = "mcp_approval_request:deny"
)

type LogMCPApprovalRequestCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RequestURN urn.MCPApprovalRequest

	// TargetRaw is the server reference as the requester named it, recorded
	// as the subject display name so a feed entry is readable without a
	// second lookup.
	TargetRaw string
}

// LogMCPApprovalRequestCreate records that an approval request was raised or
// re-raised — via mcpApproval.createRequest or promotion of a bypass request.
// A repeat ask for the same server audits as another create against the same
// subject, which is how the feed shows accumulating demand.
func (l *Logger) LogMCPApprovalRequestCreate(ctx context.Context, dbtx repo.DBTX, event LogMCPApprovalRequestCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionMCPApprovalRequestCreate),

		SubjectID:          event.RequestURN.ID.String(),
		SubjectType:        string(subjectTypeMcpApprovalRequest),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TargetRaw),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.McpApprovalRequestV1})
}

type LogMCPApprovalRequestDecideEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RequestURN urn.MCPApprovalRequest

	// Approved selects between the approve and deny actions. The decision is
	// the action rather than metadata so an auditor can filter approvals from
	// denials directly.
	Approved bool

	// TargetRaw is the server reference as the requester named it, recorded
	// as the subject display name so a feed entry is readable without a
	// second lookup.
	TargetRaw string
}

// LogMCPApprovalRequestDecide records that an MCP approval request was decided
// via mcpApproval.recordDecision. Called inside the same transaction as the
// decision insert and the request's status change, so the audit entry and the
// state it describes commit atomically.
func (l *Logger) LogMCPApprovalRequestDecide(ctx context.Context, dbtx repo.DBTX, event LogMCPApprovalRequestDecideEvent) error {
	action := ActionMCPApprovalRequestDeny
	if event.Approved {
		action = ActionMCPApprovalRequestApprove
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RequestURN.ID.String(),
		SubjectType:        string(subjectTypeMcpApprovalRequest),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TargetRaw),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.McpApprovalRequestV1})
}
