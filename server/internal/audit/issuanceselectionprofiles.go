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
	ActionIssuanceSelectionProfileCreate Action = "issuance-selection-profile:create"
	ActionIssuanceSelectionProfileUpdate Action = "issuance-selection-profile:update"
	ActionIssuanceSelectionProfileDelete Action = "issuance-selection-profile:delete"
)

type LogIssuanceSelectionProfileEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	IssuanceSelectionProfileURN  urn.IssuanceSelectionProfile
	IssuanceSelectionProfileName string

	IssuanceSelectionProfileSnapshotBefore *types.IssuanceProfile
	IssuanceSelectionProfileSnapshotAfter  *types.IssuanceProfile
}

func (l *Logger) LogIssuanceSelectionProfileCreate(ctx context.Context, dbtx repo.DBTX, event LogIssuanceSelectionProfileEvent) error {
	return l.logIssuanceSelectionProfile(ctx, dbtx, ActionIssuanceSelectionProfileCreate, event)
}

func (l *Logger) LogIssuanceSelectionProfileUpdate(ctx context.Context, dbtx repo.DBTX, event LogIssuanceSelectionProfileEvent) error {
	return l.logIssuanceSelectionProfile(ctx, dbtx, ActionIssuanceSelectionProfileUpdate, event)
}

func (l *Logger) LogIssuanceSelectionProfileDelete(ctx context.Context, dbtx repo.DBTX, event LogIssuanceSelectionProfileEvent) error {
	return l.logIssuanceSelectionProfile(ctx, dbtx, ActionIssuanceSelectionProfileDelete, event)
}

func (l *Logger) logIssuanceSelectionProfile(ctx context.Context, dbtx repo.DBTX, action Action, event LogIssuanceSelectionProfileEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.IssuanceSelectionProfileSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.IssuanceSelectionProfileSnapshotAfter)
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

		SubjectID:          event.IssuanceSelectionProfileURN.ID.String(),
		SubjectType:        string(subjectTypeIssuanceSelectionProfile),
		SubjectDisplayName: conv.ToPGTextEmpty(event.IssuanceSelectionProfileName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.IssuanceSelectionProfileV1})
}
