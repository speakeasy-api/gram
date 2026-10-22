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

const ActionOrganizationPartnerSignupAttributed Action = "organization:partner_signup_attributed"

// OrganizationPartnerSignupSnapshot records the immutable signup attribution.
type OrganizationPartnerSignupSnapshot struct {
	StripePromotionCodeID string `json:"stripe_promotion_code_id"`
}

type LogOrganizationPartnerSignupAttributedEvent struct {
	OrganizationID   string
	Actor            urn.Principal
	ActorDisplayName *string
	OrganizationName string
	OrganizationSlug string
	Attribution      OrganizationPartnerSignupSnapshot
}

// LogOrganizationPartnerSignupAttributed joins the caller's signup transaction.
// BeforeSnapshot is null because only new organizations can receive attribution.
func (l *Logger) LogOrganizationPartnerSignupAttributed(ctx context.Context, dbtx repo.DBTX, event LogOrganizationPartnerSignupAttributedEvent) error {
	after, err := marshalAuditPayload(event.Attribution)
	if err != nil {
		return fmt.Errorf("marshal partner signup attribution: %w", err)
	}
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(ActionOrganizationPartnerSignupAttributed), SubjectID: event.OrganizationID, SubjectType: "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName), SubjectSlug: conv.ToPGTextEmpty(event.OrganizationSlug),
		Metadata: nil, BeforeSnapshot: nil, AfterSnapshot: after,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationOnboardingV1})
}
