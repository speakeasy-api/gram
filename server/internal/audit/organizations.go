package audit

import (
	"context"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionOrganizationWebhooksEnabled  Action = "organization:webhooks_enabled"
	ActionOrganizationWebhooksDisabled Action = "organization:webhooks_disabled"
)

type LogOrganizationWebhooksToggledEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	WebhooksEnabled bool
}

func (l *Logger) LogOrganizationWebhooksToggled(ctx context.Context, dbtx repo.DBTX, event LogOrganizationWebhooksToggledEvent) error {
	var action Action
	if event.WebhooksEnabled {
		action = ActionOrganizationWebhooksEnabled
	} else {
		action = ActionOrganizationWebhooksDisabled
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, entry)
}
