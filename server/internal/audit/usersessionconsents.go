package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionUserSessionConsentRevoke Action = "user-session-consent:revoke"
)

type LogUserSessionConsentRevokeEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionConsentURN urn.UserSessionConsent
	Principal             urn.SessionSubject
}

func LogUserSessionConsentRevoke(ctx context.Context, dbtx repo.DBTX, event LogUserSessionConsentRevokeEvent) error {
	action := ActionUserSessionConsentRevoke
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UserSessionConsentURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionConsent),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Principal.String()),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
