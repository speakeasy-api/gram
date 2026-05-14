package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionShadowMCPApprovalRequestCreate  Action = "shadow_mcp_approval_request:create"
	ActionShadowMCPApprovalRequestApprove Action = "shadow_mcp_approval_request:approve"
	ActionShadowMCPApprovalRequestDeny    Action = "shadow_mcp_approval_request:deny"
	ActionShadowMCPAccessRuleCreate       Action = "shadow_mcp_access_rule:create"
	ActionShadowMCPAccessRuleUpdate       Action = "shadow_mcp_access_rule:update"
	ActionShadowMCPAccessRuleDelete       Action = "shadow_mcp_access_rule:delete"
)

type ShadowMCPAuditMetadata struct {
	RoleSlugs []string `json:"role_slugs,omitempty"`
	Reason    *string  `json:"reason,omitempty"`
}

type LogShadowMCPApprovalRequestEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ApprovalRequestURN urn.ShadowMCPApprovalRequest
	DisplayName        string

	ApprovalRequestSnapshotBefore *accessgen.ShadowMCPApprovalRequest
	ApprovalRequestSnapshotAfter  *accessgen.ShadowMCPApprovalRequest
	Metadata                      *ShadowMCPAuditMetadata
}

func (l *Logger) LogShadowMCPApprovalRequestCreate(ctx context.Context, dbtx repo.DBTX, event LogShadowMCPApprovalRequestEvent) error {
	return l.logShadowMCPApprovalRequest(ctx, dbtx, ActionShadowMCPApprovalRequestCreate, event)
}

func (l *Logger) LogShadowMCPApprovalRequestApprove(ctx context.Context, dbtx repo.DBTX, event LogShadowMCPApprovalRequestEvent) error {
	return l.logShadowMCPApprovalRequest(ctx, dbtx, ActionShadowMCPApprovalRequestApprove, event)
}

func (l *Logger) LogShadowMCPApprovalRequestDeny(ctx context.Context, dbtx repo.DBTX, event LogShadowMCPApprovalRequestEvent) error {
	return l.logShadowMCPApprovalRequest(ctx, dbtx, ActionShadowMCPApprovalRequestDeny, event)
}

func (l *Logger) logShadowMCPApprovalRequest(ctx context.Context, dbtx repo.DBTX, action Action, event LogShadowMCPApprovalRequestEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.ApprovalRequestSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.ApprovalRequestSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	metadata, err := marshalAuditPayload(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ApprovalRequestURN.ID.String(),
		SubjectType:        string(subjectTypeShadowMCPApprovalRequest),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ApprovalRequestURN.String()),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, entry)
}

type LogShadowMCPAccessRuleEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AccessRuleURN urn.ShadowMCPAccessRule
	DisplayName   string
	MatchValue    string

	AccessRuleSnapshotBefore *accessgen.ShadowMCPAccessRule
	AccessRuleSnapshotAfter  *accessgen.ShadowMCPAccessRule
	Metadata                 *ShadowMCPAuditMetadata
}

func (l *Logger) LogShadowMCPAccessRuleCreate(ctx context.Context, dbtx repo.DBTX, event LogShadowMCPAccessRuleEvent) error {
	return l.logShadowMCPAccessRule(ctx, dbtx, ActionShadowMCPAccessRuleCreate, event)
}

func (l *Logger) LogShadowMCPAccessRuleUpdate(ctx context.Context, dbtx repo.DBTX, event LogShadowMCPAccessRuleEvent) error {
	return l.logShadowMCPAccessRule(ctx, dbtx, ActionShadowMCPAccessRuleUpdate, event)
}

func (l *Logger) LogShadowMCPAccessRuleDelete(ctx context.Context, dbtx repo.DBTX, event LogShadowMCPAccessRuleEvent) error {
	return l.logShadowMCPAccessRule(ctx, dbtx, ActionShadowMCPAccessRuleDelete, event)
}

func (l *Logger) logShadowMCPAccessRule(ctx context.Context, dbtx repo.DBTX, action Action, event LogShadowMCPAccessRuleEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.AccessRuleSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.AccessRuleSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	metadata, err := marshalAuditPayload(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.AccessRuleURN.ID.String(),
		SubjectType:        string(subjectTypeShadowMCPAccessRule),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(event.MatchValue),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, entry)
}
