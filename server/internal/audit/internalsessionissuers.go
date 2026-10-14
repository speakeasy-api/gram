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
	ActionInternalSessionIssuerEnable  Action = "internal_session_issuer:enable"
	ActionInternalSessionIssuerDisable Action = "internal_session_issuer:disable"
)

// LogInternalSessionIssuerEvent records an organization enabling or disabling
// signing with its internal session issuer.
type LogInternalSessionIssuerEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	IssuerURN urn.InternalSessionIssuer
	Issuer    string
}

func (l *Logger) LogInternalSessionIssuerEnable(ctx context.Context, dbtx repo.DBTX, event LogInternalSessionIssuerEvent) error {
	return l.logInternalSessionIssuer(ctx, dbtx, ActionInternalSessionIssuerEnable, event)
}

func (l *Logger) LogInternalSessionIssuerDisable(ctx context.Context, dbtx repo.DBTX, event LogInternalSessionIssuerEvent) error {
	return l.logInternalSessionIssuer(ctx, dbtx, ActionInternalSessionIssuerDisable, event)
}

func (l *Logger) logInternalSessionIssuer(ctx context.Context, dbtx repo.DBTX, action Action, event LogInternalSessionIssuerEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.IssuerURN.ID.String(),
		SubjectType:        string(subjectTypeInternalSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Issuer),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.InternalSessionIssuerV1})
}
