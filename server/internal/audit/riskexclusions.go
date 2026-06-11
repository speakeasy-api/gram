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
	ActionRiskExclusionCreate Action = "risk_exclusion:create"
	ActionRiskExclusionUpdate Action = "risk_exclusion:update"
	ActionRiskExclusionDelete Action = "risk_exclusion:delete"
)

type LogRiskExclusionCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RiskExclusionID uuid.UUID //nolint:glint // matches risk_policy precedent; URN migration tracked in AGE-1954
	DisplayName     string
}

func (l *Logger) LogRiskExclusionCreate(ctx context.Context, dbtx repo.DBTX, event LogRiskExclusionCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionRiskExclusionCreate),

		SubjectID:          event.RiskExclusionID.String(),
		SubjectType:        string(subjectTypeRiskExclusion),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RiskExclusionV1})
}

type LogRiskExclusionUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RiskExclusionID uuid.UUID //nolint:glint // matches risk_policy precedent; URN migration tracked in AGE-1954
	DisplayName     string

	SnapshotBefore *types.RiskExclusion
	SnapshotAfter  *types.RiskExclusion
}

func (l *Logger) LogRiskExclusionUpdate(ctx context.Context, dbtx repo.DBTX, event LogRiskExclusionUpdateEvent) error {
	action := ActionRiskExclusionUpdate

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

		SubjectID:          event.RiskExclusionID.String(),
		SubjectType:        string(subjectTypeRiskExclusion),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RiskExclusionV1})
}

type LogRiskExclusionDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RiskExclusionID uuid.UUID //nolint:glint // matches risk_policy precedent; URN migration tracked in AGE-1954
	DisplayName     string
}

func (l *Logger) LogRiskExclusionDelete(ctx context.Context, dbtx repo.DBTX, event LogRiskExclusionDeleteEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionRiskExclusionDelete),

		SubjectID:          event.RiskExclusionID.String(),
		SubjectType:        string(subjectTypeRiskExclusion),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RiskExclusionV1})
}
