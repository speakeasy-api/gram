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

type LogSessionCaptureExclusionEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ExcludedUserID          string
	ExcludedUserDisplayName string
}

func (l *Logger) LogSessionCaptureExclusionAdd(ctx context.Context, dbtx repo.DBTX, event LogSessionCaptureExclusionEvent) error {
	return l.logSessionCaptureExclusion(ctx, dbtx, ActionSessionCaptureExclusionAdd, event)
}

func (l *Logger) LogSessionCaptureExclusionRemove(ctx context.Context, dbtx repo.DBTX, event LogSessionCaptureExclusionEvent) error {
	return l.logSessionCaptureExclusion(ctx, dbtx, ActionSessionCaptureExclusionRemove, event)
}

func (l *Logger) logSessionCaptureExclusion(ctx context.Context, dbtx repo.DBTX, action Action, event LogSessionCaptureExclusionEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

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
