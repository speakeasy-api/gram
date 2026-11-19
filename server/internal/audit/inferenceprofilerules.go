package audit

import (
	"context"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/inference_profiles"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionInferenceProfileRuleCreate Action = "inference_profile_rule:create"
	ActionInferenceProfileRuleUpdate Action = "inference_profile_rule:update"
	ActionInferenceProfileRuleDelete Action = "inference_profile_rule:delete"
)

type LogInferenceProfileRuleCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	InferenceProfileRuleURN urn.InferenceProfileRule
	// RuleDisplayName is the rule's principal URN, which is how the dashboard
	// names a rule.
	RuleDisplayName string

	InferenceProfileRuleSnapshotAfter *gen.InferenceProfileRule
}

type LogInferenceProfileRuleUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	InferenceProfileRuleURN urn.InferenceProfileRule
	RuleDisplayName         string

	InferenceProfileRuleSnapshotBefore *gen.InferenceProfileRule
	InferenceProfileRuleSnapshotAfter  *gen.InferenceProfileRule
}

type LogInferenceProfileRuleDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	InferenceProfileRuleURN urn.InferenceProfileRule
	RuleDisplayName         string

	InferenceProfileRuleSnapshotBefore *gen.InferenceProfileRule
}

func (l *Logger) LogInferenceProfileRuleCreate(ctx context.Context, dbtx repo.DBTX, event LogInferenceProfileRuleCreateEvent) error {
	after, err := marshalAuditPayload(event.InferenceProfileRuleSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionInferenceProfileRuleCreate, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      inferenceProfileAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionInferenceProfileRuleCreate, subjectTypeInferenceProfileRule, event.InferenceProfileRuleURN.ID.String(), event.RuleDisplayName, nil, after),
		OutboxEvent: events.InferenceProfileRuleV1,
	})
}

func (l *Logger) LogInferenceProfileRuleUpdate(ctx context.Context, dbtx repo.DBTX, event LogInferenceProfileRuleUpdateEvent) error {
	before, err := marshalAuditPayload(event.InferenceProfileRuleSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionInferenceProfileRuleUpdate, err)
	}
	after, err := marshalAuditPayload(event.InferenceProfileRuleSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionInferenceProfileRuleUpdate, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      inferenceProfileAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionInferenceProfileRuleUpdate, subjectTypeInferenceProfileRule, event.InferenceProfileRuleURN.ID.String(), event.RuleDisplayName, before, after),
		OutboxEvent: events.InferenceProfileRuleV1,
	})
}

func (l *Logger) LogInferenceProfileRuleDelete(ctx context.Context, dbtx repo.DBTX, event LogInferenceProfileRuleDeleteEvent) error {
	before, err := marshalAuditPayload(event.InferenceProfileRuleSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionInferenceProfileRuleDelete, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params:      inferenceProfileAuditParams(event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, ActionInferenceProfileRuleDelete, subjectTypeInferenceProfileRule, event.InferenceProfileRuleURN.ID.String(), event.RuleDisplayName, before, nil),
		OutboxEvent: events.InferenceProfileRuleV1,
	})
}
