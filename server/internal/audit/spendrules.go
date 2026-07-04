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
	ActionSpendRuleCreate Action = "spend_rule:create"
	ActionSpendRuleUpdate Action = "spend_rule:update"
	ActionSpendRuleDelete Action = "spend_rule:delete"
)

type LogSpendRuleCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SpendRuleURN  urn.SpendRule
	SpendRuleName string
}

func (l *Logger) LogSpendRuleCreate(ctx context.Context, dbtx repo.DBTX, event LogSpendRuleCreateEvent) error {
	action := ActionSpendRuleCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.SpendRuleURN.Slug,
		SubjectType:        string(subjectTypeSpendRule),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SpendRuleName),
		SubjectSlug:        conv.ToPGTextEmpty(event.SpendRuleURN.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SpendRuleV1})
}

type LogSpendRuleUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SpendRuleURN  urn.SpendRule
	SpendRuleName string

	SpendRuleSnapshotBefore *types.SpendRule
	SpendRuleSnapshotAfter  *types.SpendRule
}

func (l *Logger) LogSpendRuleUpdate(ctx context.Context, dbtx repo.DBTX, event LogSpendRuleUpdateEvent) error {
	action := ActionSpendRuleUpdate

	beforeSnapshot, err := marshalAuditPayload(event.SpendRuleSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.SpendRuleSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.SpendRuleURN.Slug,
		SubjectType:        string(subjectTypeSpendRule),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SpendRuleName),
		SubjectSlug:        conv.ToPGTextEmpty(event.SpendRuleURN.Slug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SpendRuleV1})
}

type LogSpendRuleDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SpendRuleURN  urn.SpendRule
	SpendRuleName string
}

func (l *Logger) LogSpendRuleDelete(ctx context.Context, dbtx repo.DBTX, event LogSpendRuleDeleteEvent) error {
	action := ActionSpendRuleDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.SpendRuleURN.Slug,
		SubjectType:        string(subjectTypeSpendRule),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SpendRuleName),
		SubjectSlug:        conv.ToPGTextEmpty(event.SpendRuleURN.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SpendRuleV1})
}
