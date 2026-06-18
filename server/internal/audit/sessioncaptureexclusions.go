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
	ActionSessionCaptureExclusionAdd    Action = "session_capture_exclusion:add"
	ActionSessionCaptureExclusionRemove Action = "session_capture_exclusion:remove"
)

type LogSessionCaptureExclusionAddEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ExcludedUserID          string
	ExcludedUserDisplayName string
}

type LogSessionCaptureExclusionRemoveEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ExcludedUserID          string
	ExcludedUserDisplayName string
}

func (l *Logger) LogSessionCaptureExclusionAdd(ctx context.Context, dbtx repo.DBTX, event LogSessionCaptureExclusionAddEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionSessionCaptureExclusionAdd),

		SubjectID:          event.ExcludedUserID,
		SubjectType:        string(subjectTypeSessionCaptureExclusion),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ExcludedUserDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SessionCaptureExclusionV1})
}

func (l *Logger) LogSessionCaptureExclusionRemove(ctx context.Context, dbtx repo.DBTX, event LogSessionCaptureExclusionRemoveEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionSessionCaptureExclusionRemove),

		SubjectID:          event.ExcludedUserID,
		SubjectType:        string(subjectTypeSessionCaptureExclusion),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ExcludedUserDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SessionCaptureExclusionV1})
}
