package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ActionAIToolDecisionSet records an organization deciding
// whether a detected AI tool may reach its MCP gateway. There is one verb
// rather than create/update/delete: the row is a decision, and clearing a
// decision is itself a decision (back to unreviewed), so a delete action
// would hide the moment a block was lifted.
const ActionAIToolDecisionSet Action = "ai_tool_decision:set"

type LogAIToolDecisionSetEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	DecisionURN       urn.AIToolDecision
	TargetDisplayName string

	DecisionSnapshotBefore *aitargets.DecisionRecord
	DecisionSnapshotAfter  *aitargets.DecisionRecord
}

func (l *Logger) LogAIToolDecisionSet(ctx context.Context, dbtx repo.DBTX, event LogAIToolDecisionSetEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.DecisionSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionAIToolDecisionSet, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.DecisionSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionAIToolDecisionSet, err)
	}
	return l.log(ctx, dbtx, auditEntry{
		Params: repo.InsertAuditLogParams{
			OrganizationID: event.OrganizationID,
			ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

			ActorID:          event.Actor.ID,
			ActorType:        string(event.Actor.Type),
			ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
			ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

			Action: string(ActionAIToolDecisionSet),

			SubjectID:          event.DecisionURN.ID,
			SubjectType:        string(subjectTypeAIToolDecision),
			SubjectDisplayName: conv.ToPGTextEmpty(event.TargetDisplayName),
			SubjectSlug:        conv.ToPGTextEmpty(""),

			Metadata:       nil,
			BeforeSnapshot: beforeSnapshot,
			AfterSnapshot:  afterSnapshot,
		},
		OutboxEvent: events.AIToolDecisionV1,
	})
}
