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
	ActionUserSessionRevoke Action = "user-session:revoke"
)

type LogUserSessionRevokeEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionURN urn.UserSession
	Principal      urn.SessionSubject
	Jti            string
}

func LogUserSessionRevoke(ctx context.Context, dbtx repo.DBTX, event LogUserSessionRevokeEvent) error {
	action := ActionUserSessionRevoke
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UserSessionURN.ID.String(),
		SubjectType:        string(subjectTypeUserSession),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Principal.String()),
		SubjectSlug:        conv.ToPGTextEmpty(event.Jti),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
