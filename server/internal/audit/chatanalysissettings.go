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

const ActionChatAnalysisSettingsUpsert Action = "chat_analysis_settings:upsert"

// ChatAnalysisSettingsSnapshot records one judge's switch and budget. The
// settings table is judge-keyed, so the judge name is part of the snapshot
// rather than implied by the subject.
type ChatAnalysisSettingsSnapshot struct {
	Judge    string `json:"judge"`
	Enabled  bool   `json:"enabled"`
	DailyCap int32  `json:"daily_cap"`
}

type LogChatAnalysisSettingsUpsertEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ChatAnalysisSettingsURN urn.ChatAnalysisSettings

	ChatAnalysisSettingsSnapshotBefore *ChatAnalysisSettingsSnapshot
	ChatAnalysisSettingsSnapshotAfter  *ChatAnalysisSettingsSnapshot
}

func (l *Logger) LogChatAnalysisSettingsUpsert(ctx context.Context, dbtx repo.DBTX, event LogChatAnalysisSettingsUpsertEvent) error {
	action := ActionChatAnalysisSettingsUpsert

	beforeSnapshot, err := marshalAuditPayload(event.ChatAnalysisSettingsSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.ChatAnalysisSettingsSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ChatAnalysisSettingsURN.ID,
		SubjectType:        string(subjectTypeChatAnalysisSettings),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ChatAnalysisSettingsV1})
}
