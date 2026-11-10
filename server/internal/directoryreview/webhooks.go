package directoryreview

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directoryreview/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// DBTX is the caller's transaction. It can COPY because the webhook events
// for the items a call opens and settles are enqueued in the same
// transaction, so they are delivered exactly when the change commits.
type DBTX interface {
	database.DBTX
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
}

// Event transitions, part of each event's stable delivery id.
const (
	transitionOpened   = "opened"
	transitionResolved = "resolved"
)

// publishChanges enqueues a directory_review.opened event for every item the
// call opened and a directory_review.resolved event for every item it
// superseded.
func publishChanges(ctx context.Context, dbtx DBTX, organizationID string, changes Changes) error {
	if changes.Empty() {
		return nil
	}
	return publish(ctx, dbtx, organizationID, changes.Opened, changes.Superseded)
}

// PublishResolved enqueues a directory_review.resolved event for items an
// admin approved or dismissed. Call it on the transaction that resolved them,
// after any mapping write the decision made.
func PublishResolved(ctx context.Context, dbtx DBTX, organizationID string, resolved ...Item) error {
	if len(resolved) == 0 {
		return nil
	}
	return publish(ctx, dbtx, organizationID, nil, resolved)
}

func publish(ctx context.Context, dbtx DBTX, organizationID string, opened, resolved []Item) error {
	pending, err := repo.New(dbtx).CountPendingDirectoryReviewItems(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("count pending directory review items: %w", err)
	}
	now := time.Now().UTC()

	if len(opened) > 0 {
		if _, err := outbox.PublishIdentifiedWebhookEvents(ctx, dbtx, organizationID, events.DirectoryReviewOpenedV1, webhookEvents(opened, transitionOpened, pending, now)); err != nil {
			return fmt.Errorf("publish directory review opened webhooks: %w", err)
		}
	}
	if len(resolved) > 0 {
		if _, err := outbox.PublishIdentifiedWebhookEvents(ctx, dbtx, organizationID, events.DirectoryReviewResolvedV1, webhookEvents(resolved, transitionResolved, pending, now)); err != nil {
			return fmt.Errorf("publish directory review resolved webhooks: %w", err)
		}
	}
	return nil
}

func webhookEvents(items []Item, transition string, pending int64, now time.Time) []outbox.IdentifiedWebhookEvent[events.DirectoryReviewPayloadV1] {
	out := make([]outbox.IdentifiedWebhookEvent[events.DirectoryReviewPayloadV1], 0, len(items))
	for _, item := range items {
		out = append(out, outbox.IdentifiedWebhookEvent[events.DirectoryReviewPayloadV1]{
			ID:      events.DirectoryReviewEventID(item.ID, transition),
			Payload: webhookPayload(item, pending, now),
		})
	}
	return out
}

func webhookPayload(item Item, pending int64, now time.Time) events.DirectoryReviewPayloadV1 {
	payload := events.DirectoryReviewPayloadV1{
		ID:               item.ID,
		OrganizationID:   item.OrganizationID,
		Kind:             item.Kind,
		SourceKind:       "attribute",
		DirectoryGroupID: nil,
		Occurrence:       item.Occurrence,
		State:            item.State,
		PendingCount:     pending,
		OccurredAt:       now,
	}
	if item.DirectoryGroupID.Valid {
		groupID := item.DirectoryGroupID.UUID
		payload.SourceKind = "group"
		payload.DirectoryGroupID = &groupID
	}
	return payload
}
