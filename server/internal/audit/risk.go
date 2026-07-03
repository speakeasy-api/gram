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
	ActionRiskPolicyBypassRequestApprove Action = "risk_policy:bypass_request_approve"
	ActionRiskPolicyBypassRequestCreate  Action = "risk_policy:bypass_request_create"
	ActionRiskPolicyBypassRequestDeny    Action = "risk_policy:bypass_request_deny"
	ActionRiskPolicyBypassRequestRevoke  Action = "risk_policy:bypass_request_revoke"
	ActionRiskPolicyChallengeAcknowledge Action = "risk_policy:challenge_acknowledge"
	ActionRiskPolicyCreate               Action = "risk_policy:create"
	ActionRiskPolicyUpdate               Action = "risk_policy:update"
	ActionRiskPolicyDelete               Action = "risk_policy:delete"
	ActionRiskPolicyTrigger              Action = "risk_policy:trigger"
)

type RiskPolicyBypassRequestMetadata struct {
	RequestID            string            `json:"request_id"`
	TargetKind           string            `json:"target_kind,omitempty"`
	TargetKey            string            `json:"target_key,omitempty"`
	TargetDimensions     map[string]string `json:"target_dimensions,omitempty"`
	RequesterUserID      string            `json:"requester_user_id"`
	GrantedPrincipalURNs []string          `json:"granted_principal_urns,omitempty"`
	PreviousStatus       string            `json:"previous_status,omitempty"`
	CurrentStatus        string            `json:"current_status"`
}

type RiskPolicyBypassRequestSnapshot struct {
	ID                   string            `json:"id"`
	PolicyID             string            `json:"policy_id"`
	TargetKind           *string           `json:"target_kind,omitempty"`
	TargetLabel          *string           `json:"target_label,omitempty"`
	TargetKey            *string           `json:"target_key,omitempty"`
	TargetDimensions     map[string]string `json:"target_dimensions,omitempty"`
	RequesterUserID      string            `json:"requester_user_id"`
	RequesterEmail       *string           `json:"requester_email,omitempty"`
	Note                 *string           `json:"note,omitempty"`
	Status               string            `json:"status"`
	DecidedBy            *string           `json:"decided_by,omitempty"`
	GrantedPrincipalURNs []string          `json:"granted_principal_urns,omitempty"`
	DecidedAt            *string           `json:"decided_at,omitempty"`
	CreatedAt            string            `json:"created_at"`
	UpdatedAt            string            `json:"updated_at"`
}

type LogRiskPolicyCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RiskPolicyID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.RiskPolicy and migrate to RiskPolicyURN; pending team discussion
	RiskPolicyName string
}

func (l *Logger) LogRiskPolicyCreate(ctx context.Context, dbtx repo.DBTX, event LogRiskPolicyCreateEvent) error {
	action := ActionRiskPolicyCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RiskPolicyID.String(),
		SubjectType:        string(subjectTypeRiskPolicy),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RiskPolicyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RiskPolicyV1})
}

type LogRiskPolicyUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RiskPolicyID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.RiskPolicy and migrate to RiskPolicyURN; pending team discussion
	RiskPolicyName string

	SnapshotBefore *types.RiskPolicy
	SnapshotAfter  *types.RiskPolicy
}

func (l *Logger) LogRiskPolicyUpdate(ctx context.Context, dbtx repo.DBTX, event LogRiskPolicyUpdateEvent) error {
	action := ActionRiskPolicyUpdate

	beforeSnapshot, err := marshalAuditPayload(event.SnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.SnapshotAfter)
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

		SubjectID:          event.RiskPolicyID.String(),
		SubjectType:        string(subjectTypeRiskPolicy),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RiskPolicyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RiskPolicyV1})
}

type LogRiskPolicyDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RiskPolicyID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.RiskPolicy and migrate to RiskPolicyURN; pending team discussion
	RiskPolicyName string
}

func (l *Logger) LogRiskPolicyDelete(ctx context.Context, dbtx repo.DBTX, event LogRiskPolicyDeleteEvent) error {
	action := ActionRiskPolicyDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RiskPolicyID.String(),
		SubjectType:        string(subjectTypeRiskPolicy),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RiskPolicyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RiskPolicyV1})
}

type LogRiskPolicyTriggerEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RiskPolicyID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.RiskPolicy and migrate to RiskPolicyURN; pending team discussion
	RiskPolicyName string
}

func (l *Logger) LogRiskPolicyTrigger(ctx context.Context, dbtx repo.DBTX, event LogRiskPolicyTriggerEvent) error {
	action := ActionRiskPolicyTrigger
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RiskPolicyID.String(),
		SubjectType:        string(subjectTypeRiskPolicy),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RiskPolicyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RiskPolicyV1})
}

type LogRiskPolicyBypassRequestEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RiskPolicyID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.RiskPolicy and migrate to RiskPolicyURN; pending team discussion
	RiskPolicyName string

	PolicyBypassRequestSnapshotBefore *RiskPolicyBypassRequestSnapshot
	PolicyBypassRequestSnapshotAfter  *RiskPolicyBypassRequestSnapshot
	Metadata                          *RiskPolicyBypassRequestMetadata
}

func (l *Logger) LogRiskPolicyBypassRequestCreate(ctx context.Context, dbtx repo.DBTX, event LogRiskPolicyBypassRequestEvent) error {
	return l.logRiskPolicyBypassRequest(ctx, dbtx, ActionRiskPolicyBypassRequestCreate, event)
}

func (l *Logger) LogRiskPolicyBypassRequestApprove(ctx context.Context, dbtx repo.DBTX, event LogRiskPolicyBypassRequestEvent) error {
	return l.logRiskPolicyBypassRequest(ctx, dbtx, ActionRiskPolicyBypassRequestApprove, event)
}

func (l *Logger) LogRiskPolicyBypassRequestDeny(ctx context.Context, dbtx repo.DBTX, event LogRiskPolicyBypassRequestEvent) error {
	return l.logRiskPolicyBypassRequest(ctx, dbtx, ActionRiskPolicyBypassRequestDeny, event)
}

func (l *Logger) LogRiskPolicyBypassRequestRevoke(ctx context.Context, dbtx repo.DBTX, event LogRiskPolicyBypassRequestEvent) error {
	return l.logRiskPolicyBypassRequest(ctx, dbtx, ActionRiskPolicyBypassRequestRevoke, event)
}

func (l *Logger) logRiskPolicyBypassRequest(ctx context.Context, dbtx repo.DBTX, action Action, event LogRiskPolicyBypassRequestEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.PolicyBypassRequestSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.PolicyBypassRequestSnapshotAfter)
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

		SubjectID:          event.RiskPolicyID.String(),
		SubjectType:        string(subjectTypeRiskPolicy),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RiskPolicyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RiskPolicyV1})
}
