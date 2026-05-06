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
	ActionUserSessionClientRevoke Action = "user-session-client:revoke"
)

type LogUserSessionClientRevokeEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionClientURN urn.UserSessionClient
	ClientID             string //nolint:glint // RFC 7591 client_id (DCR-issued opaque string), distinct from the resource's URN/UUID.
	ClientName           string
}

func LogUserSessionClientRevoke(ctx context.Context, dbtx repo.DBTX, event LogUserSessionClientRevokeEvent) error {
	action := ActionUserSessionClientRevoke
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UserSessionClientURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ClientName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ClientID),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
