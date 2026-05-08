package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionRiskPolicyCreate  Action = "risk_policy:create"
	ActionRiskPolicyUpdate  Action = "risk_policy:update"
	ActionRiskPolicyDelete  Action = "risk_policy:delete"
	ActionRiskPolicyTrigger Action = "risk_policy:trigger"
)

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

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
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

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
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

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
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

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
