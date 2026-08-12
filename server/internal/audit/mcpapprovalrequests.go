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
	ActionMCPApprovalRequestCreate          Action = "mcp_approval_request:create"
	ActionMCPApprovalRequestApprove         Action = "mcp_approval_request:approve"
	ActionMCPApprovalRequestDeny            Action = "mcp_approval_request:deny"
	ActionMCPApprovalRequestEvidenceChanged Action = "mcp_approval_request:evidence_changed"
)

type LogMCPApprovalRequestCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RequestURN urn.MCPApprovalRequest

	// TargetRaw is the stored (redacted) form of the server reference,
	// recorded as the subject display name so a feed entry is readable
	// without a second lookup. Callers must never pass the verbatim input:
	// this feed is immutable and re-emitted over the outbox.
	TargetRaw string
}

// LogMCPApprovalRequestCreate records that an approval request was raised or
// re-raised — via mcpApproval.createRequest, promotion of a bypass request,
// or mcpApproval.ensureServerReview inserting a fresh unreviewed evidence
// dossier (the dossier path audits only the insert itself, never a repeat
// resolve). A repeat ask for the same server audits as another create against
// the same subject, which is how the feed shows accumulating demand.
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

	// TargetRaw is the stored (redacted) form of the server reference,
	// recorded as the subject display name so a feed entry is readable
	// without a second lookup.
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

type LogMCPApprovalRequestEvidenceChangedEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	RequestURN urn.MCPApprovalRequest

	// TargetRaw is the stored (redacted) form of the server reference,
	// recorded as the subject display name so a feed entry is readable
	// without a second lookup.
	TargetRaw string

	// DiffSummary is the compact JSON rendering of what moved (scopes,
	// demanded secrets, authority mode, advisories), carried as event
	// metadata so a webhook consumer can see what changed without a
	// follow-up API call.
	DiffSummary []byte
}

// LogMCPApprovalRequestEvidenceChanged records that the daily recheck found
// the permission-relevant evidence for an approved server has drifted from
// the snapshot its latest approval rested on. The actor is the system: no
// person acted, the sweep observed. Announced once per distinct drift — the
// notified fingerprint on the request row is what dedupes, and it is written
// in the same transaction as this entry.
func (l *Logger) LogMCPApprovalRequestEvidenceChanged(ctx context.Context, dbtx repo.DBTX, event LogMCPApprovalRequestEvidenceChangedEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          "system",
		ActorType:        string(urn.PrincipalTypeUser),
		ActorDisplayName: conv.ToPGTextEmpty("Evidence recheck"),
		ActorSlug:        conv.ToPGTextEmpty(""),

		Action: string(ActionMCPApprovalRequestEvidenceChanged),

		SubjectID:          event.RequestURN.ID.String(),
		SubjectType:        string(subjectTypeMcpApprovalRequest),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TargetRaw),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       event.DiffSummary,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.McpApprovalRequestV1})
}
