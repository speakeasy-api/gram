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

// An admin's decision on a directory review item. Approving an item also
// writes directory role mappings, which carry their own audit entries.
const (
	ActionDirectoryReviewItemApprove Action = "directory_review_item:approve"
	ActionDirectoryReviewItemDismiss Action = "directory_review_item:dismiss"
	ActionDirectoryReviewItemOptOut  Action = "directory_review_item:opt_out"
)

type directoryReviewItemMetadata struct {
	Kind         string  `json:"kind"`
	Occurrence   int32   `json:"occurrence"`
	AttributeKey *string `json:"attribute_key,omitempty"`
}

type LogDirectoryReviewItemApproveEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ItemURN     urn.DirectoryReviewItem
	SourceLabel string
	Kind        string
	Occurrence  int32
}

func (l *Logger) LogDirectoryReviewItemApprove(ctx context.Context, dbtx repo.DBTX, event LogDirectoryReviewItemApproveEvent) error {
	return l.logDirectoryReviewItem(ctx, dbtx, ActionDirectoryReviewItemApprove, directoryReviewItemEntry{
		organizationID: event.OrganizationID, actor: event.Actor, actorDisplayName: event.ActorDisplayName, actorSlug: event.ActorSlug,
		itemURN: event.ItemURN, sourceLabel: event.SourceLabel,
		metadata: directoryReviewItemMetadata{Kind: event.Kind, Occurrence: event.Occurrence, AttributeKey: nil},
	})
}

type LogDirectoryReviewItemDismissEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ItemURN     urn.DirectoryReviewItem
	SourceLabel string
	Kind        string
	Occurrence  int32
}

func (l *Logger) LogDirectoryReviewItemDismiss(ctx context.Context, dbtx repo.DBTX, event LogDirectoryReviewItemDismissEvent) error {
	return l.logDirectoryReviewItem(ctx, dbtx, ActionDirectoryReviewItemDismiss, directoryReviewItemEntry{
		organizationID: event.OrganizationID, actor: event.Actor, actorDisplayName: event.ActorDisplayName, actorSlug: event.ActorSlug,
		itemURN: event.ItemURN, sourceLabel: event.SourceLabel,
		metadata: directoryReviewItemMetadata{Kind: event.Kind, Occurrence: event.Occurrence, AttributeKey: nil},
	})
}

// LogDirectoryReviewItemOptOutEvent records an admin dismissing a new
// attribute value and opting its key out of new-value decisions.
type LogDirectoryReviewItemOptOutEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ItemURN      urn.DirectoryReviewItem
	SourceLabel  string
	Kind         string
	Occurrence   int32
	AttributeKey string
}

func (l *Logger) LogDirectoryReviewItemOptOut(ctx context.Context, dbtx repo.DBTX, event LogDirectoryReviewItemOptOutEvent) error {
	return l.logDirectoryReviewItem(ctx, dbtx, ActionDirectoryReviewItemOptOut, directoryReviewItemEntry{
		organizationID: event.OrganizationID, actor: event.Actor, actorDisplayName: event.ActorDisplayName, actorSlug: event.ActorSlug,
		itemURN: event.ItemURN, sourceLabel: event.SourceLabel,
		metadata: directoryReviewItemMetadata{Kind: event.Kind, Occurrence: event.Occurrence, AttributeKey: &event.AttributeKey},
	})
}

type directoryReviewItemEntry struct {
	organizationID   string
	actor            urn.Principal
	actorDisplayName *string
	actorSlug        *string
	itemURN          urn.DirectoryReviewItem
	sourceLabel      string
	metadata         directoryReviewItemMetadata
}

func (l *Logger) logDirectoryReviewItem(ctx context.Context, dbtx repo.DBTX, action Action, e directoryReviewItemEntry) error {
	metadata, err := marshalAuditPayload(&e.metadata)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: e.organizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          e.actor.ID,
		ActorType:        string(e.actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(e.actorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(e.actorSlug),

		Action: string(action),

		SubjectID:          e.itemURN.ID.String(),
		SubjectType:        string(subjectTypeDirectoryReviewItem),
		SubjectDisplayName: conv.ToPGTextEmpty(e.sourceLabel),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DirectoryReviewItemV1})
}
