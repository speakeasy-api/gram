package mcpevents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	mcpeventsv1 "github.com/speakeasy-api/gram/infra/gen/gram/mcpevents/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpevents/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/streams"
	"golang.org/x/sync/errgroup"
)

const (
	// maxDeliveryAttempts bounds retries even if a broker redelivers repeatedly.
	maxDeliveryAttempts = 8
	// deliveryLifetime prevents an old review hint from waking a client days later.
	deliveryLifetime = 24 * time.Hour
)

// PublishReview records an occurrence in the domain transaction. Decision
// recipients are frozen here so subsequent requests cannot acquire old events.
func PublishReview(ctx context.Context, tx pgx.Tx, organizationID string, event Event) error {
	recipients := []string{}
	if event.Name == ReviewDecided {
		var err error
		recipients, err = repo.New(tx).ListReviewEventRecipients(ctx, repo.ListReviewEventRecipientsParams{RequestID: event.Data.RequestID, ProjectID: event.Data.ProjectID, OrganizationID: organizationID})
		if err != nil {
			return fmt.Errorf("read review recipients: %w", err)
		}
	}
	_, err := outbox.Publish(ctx, tx, organizationID, outbox.Message{
		Proto: mcpeventsv1.ReviewChanged_builder{
			EventId: new(event.EventID.String()), OrganizationId: new(organizationID), ProjectId: new(event.Data.ProjectID.String()),
			RequestId: new(event.Data.RequestID.String()), DecisionId: new(event.Data.DecisionID), EventName: new(event.Name),
			OccurredAt: new(event.Timestamp.UTC().Format(time.RFC3339Nano)), RecipientUserIds: recipients,
		}.Build(), PublicID: event.EventID, Attributes: nil,
	})
	if err != nil {
		return fmt.Errorf("publish review event: %w", err)
	}
	return nil
}

// Route persists an idempotent delivery and outbox handoff per matching subscription.
func (s *Service) Route(ctx context.Context, message *mcpeventsv1.ReviewChanged, _ gcp.MessageMetadata) error {
	eventID, eventErr := uuid.Parse(message.GetEventId())
	projectID, projectErr := uuid.Parse(message.GetProjectId())
	requestID, requestErr := uuid.Parse(message.GetRequestId())
	occurredAt, timeErr := time.Parse(time.RFC3339Nano, message.GetOccurredAt())
	if eventErr != nil || projectErr != nil || requestErr != nil || timeErr != nil || message.GetOrganizationId() == "" || (message.GetEventName() != ReviewRequested && message.GetEventName() != ReviewDecided) {
		return nil // Invalid producer messages cannot become valid on retry.
	}
	now := s.now()
	if now.Sub(occurredAt) > deliveryLifetime {
		return nil
	}
	event := Event{EventID: eventID, Name: message.GetEventName(), Timestamp: occurredAt, Data: ReviewData{ProjectID: projectID, RequestID: requestID, DecisionID: message.GetDecisionId()}, Cursor: nil}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin event routing: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	subscriptions, err := queries.MatchSubscriptions(ctx, repo.MatchSubscriptionsParams{
		OrganizationID: message.GetOrganizationId(), ProjectID: projectID, EventName: event.Name,
		OccurredAt: conv.ToPGTimestamptz(occurredAt), Now: conv.ToPGTimestamptz(now), Recipients: message.GetRecipientUserIds(),
	})
	if err != nil {
		return fmt.Errorf("match event subscriptions: %w", err)
	}
	for _, subscription := range subscriptions {
		id, err := queries.CreateDelivery(ctx, repo.CreateDeliveryParams{
			ID: uuid.New(), OrganizationID: subscription.OrganizationID, ProjectID: projectID,
			SubscriptionID: conv.ToPGText(subscription.ID), SubscriptionGeneration: subscription.Generation,
			EventID: eventID, EventName: event.Name, RequestID: requestID, Payload: payload,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("record event delivery: %w", err)
		}
		_, err = outbox.Publish(ctx, tx, subscription.OrganizationID, outbox.Message{
			Proto: mcpeventsv1.Delivery_builder{Id: new(id.String()), OrganizationId: new(subscription.OrganizationID), ProjectId: new(projectID.String())}.Build(), PublicID: id, Attributes: nil,
		})
		if err != nil {
			return fmt.Errorf("enqueue event delivery: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit event routing: %w", err)
	}
	return nil
}

// Deliver rechecks current authority before each attempt. Subscription locks
// serialize delivery with unsubscribe: no send can start after it returns.
func (s *Service) Deliver(ctx context.Context, message *mcpeventsv1.Delivery, _ gcp.MessageMetadata) error {
	id, idErr := uuid.Parse(message.GetId())
	projectID, projectErr := uuid.Parse(message.GetProjectId())
	if idErr != nil || projectErr != nil || message.GetOrganizationId() == "" {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin event delivery: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	delivery, err := queries.GetDeliveryForUpdate(ctx, repo.GetDeliveryForUpdateParams{ID: id, ProjectID: projectID, OrganizationID: message.GetOrganizationId()})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read event delivery: %w", err)
	}
	if delivery.Status != "pending" {
		return nil
	}
	if err := queries.LockSubscriptionIdentity(ctx, delivery.SubscriptionID.String); err != nil {
		return fmt.Errorf("lock delivery subscription: %w", err)
	}
	subscription, err := queries.GetSubscription(ctx, repo.GetSubscriptionParams{ID: delivery.SubscriptionID.String, ProjectID: projectID, OrganizationID: delivery.OrganizationID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read delivery subscription: %w", err)
	}
	now := s.now().UTC()
	status := "suppressed"
	retry := false
	if err == nil && !subscription.DeletedAt.Valid && subscription.Generation == delivery.SubscriptionGeneration && subscription.ExpiresAt.Time.After(now) && delivery.CreatedAt.Time.Add(deliveryLifetime).After(now) && delivery.Attempts < maxDeliveryAttempts {
		authErr := s.authorizer.WithTx(tx).Authorize(ctx, subscriptionOwner(subscription), projectID, subscription.EventName, delivery.RequestID)
		if authErr != nil && !errors.Is(authErr, ErrDenied) {
			return fmt.Errorf("authorize delivery: %w", authErr)
		}
		if authErr == nil {
			secret, err := s.encryption.Decrypt(subscription.SigningSecret)
			if err != nil {
				return fmt.Errorf("decrypt delivery key: %w", err)
			}
			keys := []string{secret}
			if subscription.PreviousSigningSecret.Valid && subscription.PreviousSecretExpiresAt.Time.After(now) {
				previous, err := s.encryption.Decrypt(subscription.PreviousSigningSecret.String)
				if err != nil {
					return fmt.Errorf("decrypt rotating delivery key: %w", err)
				}
				keys = append(keys, previous)
			}
			code, _, sendErr := s.webhook.send(ctx, subscription.ID, delivery.EventID.String(), subscription.CallbackUrl, keys, now, delivery.Payload)
			status = "failed"
			if sendErr == nil && code >= http.StatusOK && code < http.StatusMultipleChoices {
				status = "delivered"
			} else if code != http.StatusGone && code != http.StatusRequestEntityTooLarge && (sendErr != nil || code == http.StatusRequestTimeout || code == http.StatusTooEarly || code == http.StatusTooManyRequests || code >= http.StatusInternalServerError) && delivery.Attempts+1 < maxDeliveryAttempts {
				status, retry = "pending", true
			}
		}
	}
	if err := queries.SetDeliveryOutcome(ctx, repo.SetDeliveryOutcomeParams{ID: id, ProjectID: projectID, OrganizationID: delivery.OrganizationID, Status: status, Now: conv.ToPGTimestamptz(now)}); err != nil {
		return fmt.Errorf("record delivery outcome: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delivery outcome: %w", err)
	}
	if retry {
		return errors.New("event callback temporarily unavailable")
	}
	return nil
}

// RouteBatch settles individual source events independently after durable fan-out.
func (s *Service) RouteBatch(ctx context.Context, messages []streams.BatchMessage[*mcpeventsv1.ReviewChanged]) error {
	for _, message := range messages {
		message.Fail(s.Route(ctx, message.Message, message.Metadata))
	}
	return nil
}

// DeliverBatch bounds concurrent callbacks and lets the broker back off failed deliveries.
func (s *Service) DeliverBatch(ctx context.Context, messages []streams.BatchMessage[*mcpeventsv1.Delivery]) error {
	// Four concurrent receivers cap held database connections per delivery batch.
	const callbackConcurrency = 4
	var group errgroup.Group
	group.SetLimit(callbackConcurrency)
	for _, message := range messages {
		group.Go(func() error { message.Fail(s.Deliver(ctx, message.Message, message.Metadata)); return nil })
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf("deliver event batch: %w", err)
	}
	return nil
}
