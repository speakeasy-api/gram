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
	ActionRemoteSessionDelete Action = "remote-session:delete"
)

type LogRemoteSessionDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteSessionURN urn.RemoteSession
	PrincipalURN     urn.SessionSubject
}

func LogRemoteSessionDelete(ctx context.Context, dbtx repo.DBTX, event LogRemoteSessionDeleteEvent) error {
	action := ActionRemoteSessionDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RemoteSessionURN.ID.String(),
		SubjectType:        string(subjectTypeRemoteSession),
		SubjectDisplayName: conv.ToPGTextEmpty(event.PrincipalURN.String()),
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
