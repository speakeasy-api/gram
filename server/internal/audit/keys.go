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
	ActionKeyCreate Action = "api_key:create"
	ActionKeyRevoke Action = "api_key:revoke"
)

type LogKeyCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.APIKey
	KeyName string

	Scopes []string
}

func (l *Logger) LogKeyCreate(ctx context.Context, dbtx repo.DBTX, event LogKeyCreateEvent) error {
	action := ActionKeyCreate

	metadata, err := marshalAuditPayload(map[string]any{
		"scopes": event.Scopes,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeAPIKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}
	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogKeyRevokeEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.APIKey
	KeyName string

	Scopes []string
}

func (l *Logger) LogKeyRevoke(ctx context.Context, dbtx repo.DBTX, event LogKeyRevokeEvent) error {
	action := ActionKeyRevoke

	metadata, err := marshalAuditPayload(map[string]any{
		"scopes": event.Scopes,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeAPIKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}
	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
