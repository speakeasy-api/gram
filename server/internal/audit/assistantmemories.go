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
	ActionAssistantMemoryCreate Action = "assistant_memory:create"
	ActionAssistantMemoryDelete Action = "assistant_memory:delete"
)

const assistantMemoryContentPreviewMax = 200

type LogAssistantMemoryCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	MemoryID    uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.AssistantMemory and migrate to MemoryURN; pending team discussion
	AssistantID uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Assistant and migrate to AssistantURN; pending team discussion
	ThreadID    uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Thread and migrate to ThreadURN; pending team discussion

	ContentPreview string
}

func (l *Logger) LogAssistantMemoryCreate(ctx context.Context, dbtx repo.DBTX, event LogAssistantMemoryCreateEvent) error {
	action := ActionAssistantMemoryCreate

	threadID := ""
	if event.ThreadID != uuid.Nil {
		threadID = event.ThreadID.String()
	}

	metadata, err := marshalAuditPayload(map[string]any{
		"assistant_id":    event.AssistantID.String(),
		"thread_id":       threadID,
		"content_preview": truncateForAudit(event.ContentPreview, assistantMemoryContentPreviewMax),
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.MemoryID.String(),
		SubjectType:        string(subjectTypeAssistantMemory),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
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

type LogAssistantMemoryDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	MemoryID    uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.AssistantMemory and migrate to MemoryURN; pending team discussion
	AssistantID uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Assistant and migrate to AssistantURN; pending team discussion
	ThreadID    uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Thread and migrate to ThreadURN; pending team discussion

	Reason string
}

func (l *Logger) LogAssistantMemoryDelete(ctx context.Context, dbtx repo.DBTX, event LogAssistantMemoryDeleteEvent) error {
	action := ActionAssistantMemoryDelete

	threadID := ""
	if event.ThreadID != uuid.Nil {
		threadID = event.ThreadID.String()
	}

	metadata, err := marshalAuditPayload(map[string]any{
		"assistant_id": event.AssistantID.String(),
		"thread_id":    threadID,
		"reason":       event.Reason,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.MemoryID.String(),
		SubjectType:        string(subjectTypeAssistantMemory),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
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

func truncateForAudit(s string, limit int) string {
	truncated := conv.TruncateString(s, limit)
	if truncated == s {
		return s
	}
	return truncated + "…"
}
