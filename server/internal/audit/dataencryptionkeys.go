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
	ActionDataEncryptionKeyCreate Action = "data_encryption_key:create"
	ActionDataEncryptionKeyRetire Action = "data_encryption_key:retire"
)

// LogDataEncryptionKeyEvent records a data encryption key being created for a
// use case, or retired to decrypt-only by a rotation. Key material is never
// part of the event.
type LogDataEncryptionKeyEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN urn.DataEncryptionKey

	// UseCase is the registered consumer the key encrypts for.
	UseCase string
}

func (l *Logger) LogDataEncryptionKeyCreate(ctx context.Context, dbtx repo.DBTX, event LogDataEncryptionKeyEvent) error {
	return l.logDataEncryptionKey(ctx, dbtx, ActionDataEncryptionKeyCreate, event)
}

func (l *Logger) LogDataEncryptionKeyRetire(ctx context.Context, dbtx repo.DBTX, event LogDataEncryptionKeyEvent) error {
	return l.logDataEncryptionKey(ctx, dbtx, ActionDataEncryptionKeyRetire, event)
}

func (l *Logger) logDataEncryptionKey(ctx context.Context, dbtx repo.DBTX, action Action, event LogDataEncryptionKeyEvent) error {
	entry := repo.InsertAuditLogParams{
		IsInternal:     false,
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeDataEncryptionKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.UseCase),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DataEncryptionKeyV1})
}
