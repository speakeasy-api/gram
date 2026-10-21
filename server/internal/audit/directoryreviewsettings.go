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

// An admin turning the organization's directory review emails on or off.
const ActionDirectoryReviewSettingsUpdate Action = "directory_review_settings:update"

// DirectoryReviewSettingsSnapshot is an organization's directory review
// settings. The subject is the organization, which has one set.
type DirectoryReviewSettingsSnapshot struct {
	EmailsEnabled bool `json:"emails_enabled"`
}

type LogDirectoryReviewSettingsUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	DirectoryReviewSettingsSnapshotBefore *DirectoryReviewSettingsSnapshot
	DirectoryReviewSettingsSnapshotAfter  *DirectoryReviewSettingsSnapshot
}

func (l *Logger) LogDirectoryReviewSettingsUpdate(ctx context.Context, dbtx repo.DBTX, event LogDirectoryReviewSettingsUpdateEvent) error {
	action := ActionDirectoryReviewSettingsUpdate

	beforeSnapshot, err := marshalAuditPayload(event.DirectoryReviewSettingsSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.DirectoryReviewSettingsSnapshotAfter)
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

		SubjectID:          event.OrganizationID,
		SubjectType:        string(subjectTypeDirectoryReviewSettings),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DirectoryReviewSettingsV1})
}
