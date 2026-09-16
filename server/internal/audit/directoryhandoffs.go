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

const (
	ActionDirectoryHandoffSet     Action = "directory_handoff:set"
	ActionDirectoryHandoffCleared Action = "directory_handoff:cleared"
)

type LogDirectoryHandoffEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	DirectoryHandoffURN urn.DirectoryHandoff
	BaseURLHost         string
	TokenFingerprint    string
}

func (l *Logger) LogDirectoryHandoffSet(ctx context.Context, dbtx repo.DBTX, event LogDirectoryHandoffEvent) error {
	return l.logDirectoryHandoff(ctx, dbtx, ActionDirectoryHandoffSet, event)
}

func (l *Logger) LogDirectoryHandoffCleared(ctx context.Context, dbtx repo.DBTX, event LogDirectoryHandoffEvent) error {
	return l.logDirectoryHandoff(ctx, dbtx, ActionDirectoryHandoffCleared, event)
}

func (l *Logger) logDirectoryHandoff(ctx context.Context, dbtx repo.DBTX, action Action, event LogDirectoryHandoffEvent) error {
	metadata, err := marshalAuditPayload(map[string]string{
		"base_url_host":     event.BaseURLHost,
		"token_fingerprint": event.TokenFingerprint,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.DirectoryHandoffURN.ID,
		SubjectType:        string(subjectTypeDirectoryHandoff),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DirectoryHandoffV1})
}
