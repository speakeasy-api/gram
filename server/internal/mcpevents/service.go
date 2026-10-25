package mcpevents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/mcpevents/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Service persists subscriptions and shares authorization with asynchronous delivery.
type Service struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	encryption *encryption.Client
	authorizer Authorizer
	webhook    *Webhook
	now        func() time.Time
}

// NewService requires live authorization; a nil dependency is a configuration error.
func NewService(logger *slog.Logger, db *pgxpool.Pool, encryptionClient *encryption.Client, authorizer Authorizer, webhook *Webhook) (*Service, error) {
	if logger == nil || db == nil || encryptionClient == nil || authorizer == nil || webhook == nil {
		return nil, errors.New("event service dependencies are required")
	}
	return &Service{logger: logger, db: db, encryption: encryptionClient, authorizer: authorizer, webhook: webhook, now: time.Now}, nil
}

// ValidateInput rejects unsupported filters and unbounded lifetimes before I/O.
func ValidateInput(input SubscriptionInput, subscribing bool) error {
	if input.Name != ReviewRequested && input.Name != ReviewDecided {
		return ErrNotFound
	}
	if input.Arguments.ProjectID == uuid.Nil {
		return errors.New("project_id must be an explicit project UUID")
	}
	if input.TTLMs != nil && *input.TTLMs <= 0 {
		return errors.New("ttlMs must be positive")
	}
	return ValidateDestination(input.Delivery, subscribing)
}

// Subscribe verifies the endpoint and atomically creates or refreshes its lifetime.
func (s *Service) Subscribe(ctx context.Context, owner Owner, input SubscriptionInput) (SubscriptionResult, error) {
	if err := ValidateInput(input, true); err != nil {
		return SubscriptionResult{}, err
	}
	projectID := input.Arguments.ProjectID
	if err := s.authorizer.Authorize(ctx, owner, projectID, input.Name, uuid.Nil); err != nil {
		return SubscriptionResult{}, fmt.Errorf("authorize subscription: %w", err)
	}
	id := SubscriptionID(owner, input)
	// Verification must not hold pooled connections or subscription/quota locks.
	verifiedAt := s.now().UTC()
	if err := s.webhook.Verify(ctx, id, input.Delivery, verifiedAt); err != nil {
		return SubscriptionResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return SubscriptionResult{}, fmt.Errorf("begin subscription: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	// The owner/project lock makes the active-subscription limit atomic across
	// distinct callback URLs, without serializing other users' subscriptions.
	if err := queries.LockSubscriptionIdentity(ctx, owner.ConnectionID.String()+":"+projectID.String()); err != nil {
		return SubscriptionResult{}, fmt.Errorf("lock subscription quota: %w", err)
	}
	if err := queries.LockSubscriptionIdentity(ctx, id); err != nil {
		return SubscriptionResult{}, fmt.Errorf("lock subscription: %w", err)
	}
	now := s.now().UTC()
	old, err := queries.GetSubscription(ctx, repo.GetSubscriptionParams{ID: id, ProjectID: projectID, OrganizationID: owner.OrganizationID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SubscriptionResult{}, fmt.Errorf("read subscription: %w", err)
	}
	active := err == nil && !old.DeletedAt.Valid && old.ExpiresAt.Time.After(now)
	if !active {
		count, err := queries.CountActiveSubscriptions(ctx, repo.CountActiveSubscriptionsParams{ProjectID: projectID, OrganizationID: owner.OrganizationID, ConnectionID: conv.ToNullUUID(owner.ConnectionID), ConnectionGeneration: owner.Generation, Now: conv.ToPGTimestamptz(now)})
		if err != nil {
			return SubscriptionResult{}, fmt.Errorf("count subscriptions: %w", err)
		}
		// Twenty subscriptions per connection/project leave room for several chats
		// while bounding persistent callbacks from a compromised client.
		const maxSubscriptions = 20
		if count >= maxSubscriptions {
			return SubscriptionResult{}, ErrLimit
		}
	}
	currentSecret := ""
	if active {
		currentSecret, err = s.encryption.Decrypt(old.SigningSecret)
		if err != nil {
			return SubscriptionResult{}, fmt.Errorf("decrypt subscription key: %w", err)
		}
	}
	if err := s.authorizer.WithTx(tx).Authorize(ctx, owner, projectID, input.Name, uuid.Nil); err != nil {
		return SubscriptionResult{}, fmt.Errorf("reauthorize subscription: %w", err)
	}
	now = s.now().UTC()
	active = active && old.ExpiresAt.Time.After(now)
	ttl := DefaultTTL
	if input.TTLMs != nil {
		// Clamp in milliseconds before conversion to avoid duration overflow.
		ttl = time.Duration(min(DefaultTTL.Milliseconds(), max(MinimumTTL.Milliseconds(), *input.TTLMs))) * time.Millisecond
	}
	key, err := s.encryption.Encrypt([]byte(input.Delivery.Secret))
	if err != nil {
		return SubscriptionResult{}, fmt.Errorf("encrypt subscription key: %w", err)
	}
	generation := uuid.New()
	subscribedAt := now
	var previousKey pgtype.Text
	var previousExpiry pgtype.Timestamptz
	if active {
		generation, subscribedAt = old.Generation, old.SubscribedAt.Time
		previousKey, previousExpiry = old.PreviousSigningSecret, old.PreviousSecretExpiresAt
		if currentSecret != input.Delivery.Secret {
			previousKey = conv.ToPGText(old.SigningSecret)
			previousExpiry = conv.ToPGTimestamptz(now.Add(RotationWindow))
		}
	}
	expiresAt := now.Add(ttl)
	_, err = queries.SaveSubscription(ctx, repo.SaveSubscriptionParams{
		ID: id, ProjectID: projectID, OrganizationID: owner.OrganizationID,
		ConnectionID: conv.ToNullUUID(owner.ConnectionID), ConnectionGeneration: owner.Generation,
		ClientID: owner.ClientID, UserID: owner.UserID, EventName: input.Name, CallbackUrl: input.Delivery.URL,
		SigningSecret: key, PreviousSigningSecret: previousKey, PreviousSecretExpiresAt: previousExpiry,
		Generation: generation, SubscribedAt: conv.ToPGTimestamptz(subscribedAt),
		ExpiresAt: conv.ToPGTimestamptz(expiresAt), VerifiedAt: conv.ToPGTimestamptz(verifiedAt),
	})
	if err != nil {
		return SubscriptionResult{}, fmt.Errorf("save subscription: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SubscriptionResult{}, fmt.Errorf("commit subscription: %w", err)
	}
	return SubscriptionResult{ID: id, RefreshBefore: expiresAt, Cursor: nil, Truncated: input.Cursor != nil}, nil
}

// Unsubscribe is owner-bound and idempotent, even after project permissions change.
// The transport still requires a live, authenticated OAuth connection and membership.
func (s *Service) Unsubscribe(ctx context.Context, owner Owner, input SubscriptionInput) error {
	if err := ValidateInput(input, false); err != nil {
		return err
	}
	id := SubscriptionID(owner, input)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin unsubscribe: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	if err := queries.LockSubscriptionIdentity(ctx, id); err != nil {
		return fmt.Errorf("lock unsubscribe: %w", err)
	}
	if err := queries.Unsubscribe(ctx, repo.UnsubscribeParams{ID: id, ProjectID: input.Arguments.ProjectID, OrganizationID: owner.OrganizationID, Now: conv.ToPGTimestamptz(s.now())}); err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit unsubscribe: %w", err)
	}
	return nil
}

func subscriptionOwner(row repo.PlatformMcpEventSubscription) Owner {
	return Owner{OrganizationID: row.OrganizationID, UserID: row.UserID, ConnectionID: row.ConnectionID.UUID, Generation: row.ConnectionGeneration, ClientID: row.ClientID}
}
