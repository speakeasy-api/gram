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
	ActionRiskResultUnmask Action = "risk_result:unmask"
)

type LogRiskResultUnmaskEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RiskResultID uuid.UUID //nolint:glint // matches risk_policy precedent; URN migration tracked in AGE-1954
	// ChatID is recorded so reviewers can see which chat the unmasked secret
	// came from without a second lookup. It is auxiliary context, not the
	// audit subject (which is RiskResultID).
	ChatID uuid.UUID //nolint:glint // auxiliary context, not the audit subject
}

// LogRiskResultUnmask records that a risk result's plaintext match was
// revealed via risk.unmaskResult. Unlike most audit events this describes a
// read, not a mutation, so callers pass the pool directly as dbtx — there is
// no surrounding transaction to be atomic with.
func (l *Logger) LogRiskResultUnmask(ctx context.Context, dbtx repo.DBTX, event LogRiskResultUnmaskEvent) error {
	action := ActionRiskResultUnmask

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RiskResultID.String(),
		SubjectType:        string(subjectTypeRiskResult),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(event.ChatID.String()),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RiskResultV1})
}
