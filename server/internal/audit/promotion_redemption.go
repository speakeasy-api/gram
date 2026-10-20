package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const ActionOrganizationPromotionRedeemed Action = "organization:promotion_redeemed"

// OrganizationPromotionRedemptionSnapshot records a completed promotion redemption.
type OrganizationPromotionRedemptionSnapshot struct {
	StripePromotionCodeID string `json:"stripe_promotion_code_id"`
	StripeSubscriptionID  string `json:"stripe_subscription_id"`
}

type LogOrganizationPromotionRedeemedEvent struct {
	OrganizationID   string
	Actor            urn.Principal
	ActorDisplayName *string
	OrganizationName string
	OrganizationSlug string
	Redemption       OrganizationPromotionRedemptionSnapshot
}

// LogOrganizationPromotionRedeemed joins the caller's activation transaction.
// BeforeSnapshot is null because each redemption is an immutable new record.
func (l *Logger) LogOrganizationPromotionRedeemed(ctx context.Context, dbtx repo.DBTX, event LogOrganizationPromotionRedeemedEvent) error {
	after, err := marshalAuditPayload(event.Redemption)
	if err != nil {
		return fmt.Errorf("marshal promotion redemption: %w", err)
	}
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(ActionOrganizationPromotionRedeemed), SubjectID: event.OrganizationID, SubjectType: "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName), SubjectSlug: conv.ToPGTextEmpty(event.OrganizationSlug),
		Metadata: nil, BeforeSnapshot: nil, AfterSnapshot: after,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationBillingV1})
}
