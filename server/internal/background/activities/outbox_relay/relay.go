// Package outbox_relay drains the global event outbox.
//
// Activities here back the ProcessOutboxWorkflow defined in the parent
// background package. The workflow runs continuously: each iteration fetches
// a batch of pending outbox IDs (returning a HasMore flag to signal back-
// pressure), filters out rows for orgs with no Svix app (marking them noop
// in the DB), and relays the remainder to Svix. Per-row delivery failures
// are persisted in the DB and retried on the next iteration; rows exceeding
// MaxAttempts are dead-lettered. Infrastructure failures (DB unavailable)
// are returned as activity errors so Temporal can retry the activity.
//
// Only IDs and the svix_app_id cross activity boundaries to keep Temporal
// history payloads small — full rows are re-queried inside RelayEvents.
package outbox_relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	svix "github.com/svix/svix-webhooks/go"
	"github.com/svix/svix-webhooks/go/models"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// maxBatchSize is the max number of outbox rows fetched per workflow run.
const maxBatchSize int32 = 50

// maxAttempts caps retry attempts before a row is dead-lettered.
const maxAttempts int32 = 10

const (
	retryBaseDelay = 5 * time.Second
	retryMaxDelay  = 10 * time.Minute
)

// calcRetryAfter returns a jittered retry timestamp using full-jitter
// exponential back-off: delay = random(0, min(cap, base * 2^attempts)).
// Jitter prevents a wave of failures from all becoming eligible again
// simultaneously (thundering herd).
func calcRetryAfter(attempts int32) pgtype.Timestamptz {
	exp := min(
		// cap shift to avoid overflow
		retryBaseDelay*(1<<min(attempts, 20)), retryMaxDelay)
	jitter := time.Duration(rand.Int64N(int64(exp))) // #nosec G404 - retry jitter is not security-sensitive
	return pgtype.Timestamptz{Time: time.Now().Add(jitter), InfinityModifier: pgtype.Finite, Valid: true}
}

type FetchEventArgs struct{}

// FetchEventsResult is returned by FetchEvents. HasMore is true when the
// outbox has more rows beyond this batch (N+1 probe).
type FetchEventsResult struct {
	Events  []*Event
	HasMore bool
}

type Event struct {
	OutboxID        int64
	OrganizationID  string
	SvixAppID       string
	WebhooksEnabled bool
}

type Relay struct {
	logger      *slog.Logger
	tracer      trace.Tracer
	db          *pgxpool.Pool
	svixClient  *svix.Svix
	maxAttempts int32
	features    *productfeatures.Client
}

func New(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, svixClient *svix.Svix, features *productfeatures.Client) *Relay {
	return &Relay{
		logger:      logger,
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/outbox_relay"),
		db:          db,
		svixClient:  svixClient,
		features:    features,
		maxAttempts: maxAttempts,
	}
}

func (r *Relay) FetchEvents(ctx context.Context, args FetchEventArgs) (FetchEventsResult, error) {
	q := repo.New(r.db)

	// Fetch one extra row to detect whether more remain beyond this batch.
	rows, err := q.FetchPendingOutboxIDs(ctx, maxBatchSize+1)
	if err != nil {
		return FetchEventsResult{}, fmt.Errorf("fetch pending outbox ids: %w", err)
	}

	hasMore := len(rows) > int(maxBatchSize)
	if hasMore {
		rows = rows[:maxBatchSize]
	}

	results := make([]*Event, 0, len(rows))
	for _, row := range rows {
		results = append(results, &Event{
			OutboxID:        row.ID,
			OrganizationID:  row.OrganizationID,
			SvixAppID:       row.SvixAppID.String,
			WebhooksEnabled: row.WebhooksEnabled.Bool,
		})
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OutboxBatchSize(len(results)),
	)

	return FetchEventsResult{Events: results, HasMore: hasMore}, nil
}

func (r *Relay) FilterNoopEvents(ctx context.Context, events []*Event) ([]*Event, error) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attr.OutboxBatchSize(len(events)),
	)

	if len(events) == 0 {
		return nil, nil
	}

	// Implementor's note: SQLc does not support bulk INSERT of rows. It does
	// support COPY protocol but that is all or nothing and would fail a whole
	// batch if even one row has a constraint violation. We need upsert
	// semantics to efficiently processing the outbox.
	// This was not a vibe coded design decision but a deliberate one.

	psql := squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
	stmt := psql.Insert("outbox_relays").
		Columns("outbox_id", "processed_at", "noop", "attempts", "last_error")

	total := 0
	validEvents := make([]*Event, 0, len(events))
	featureCheck := make(map[string]bool, len(events))
	for _, res := range events {
		enabled, err := areWebhooksEnabled(ctx, r.features, featureCheck, res)
		if err != nil {
			return nil, err
		}

		if enabled {
			validEvents = append(validEvents, res)
			continue
		}

		stmt = stmt.Values(res.OutboxID, squirrel.Expr("clock_timestamp()"), true, 1, nil)
		total++
	}

	if total == 0 {
		span.SetAttributes(attr.OutboxNoopRows(0))
		return validEvents, nil
	}

	stmt = stmt.Suffix(`
		ON CONFLICT (outbox_id) DO UPDATE SET
			processed_at = clock_timestamp(),
			noop = TRUE,
			attempts = outbox_relays.attempts + 1,
			last_error = NULL,
			updated_at = clock_timestamp()`)

	query, args, err := stmt.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build mark batch noop sql: %w", err)
	}

	res, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("execute mark batch noop sql: %w", err)
	}

	span.SetAttributes(attr.OutboxNoopRows(int(res.RowsAffected())))

	return validEvents, nil
}

func (r *Relay) RelayEvents(ctx context.Context, events []*Event) error {
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OutboxBatchSize(len(events)),
	)

	if len(events) == 0 {
		return nil
	}

	// FilterNoopSvixEvents must have already marked events without SvixAppID or
	// WebhooksEnabled as noop. The re-check here is defensive: any event that
	// slips through would be silently skipped by FetchOutboxRowsByIDs and left
	// in a pending state forever, so we guard against that regression here.
	filtered := make([]int64, 0, len(events))
	appIDs := make(map[string]string, len(events)) // orgID -> svixAppID
	for _, res := range events {
		if res.SvixAppID != "" && res.WebhooksEnabled {
			filtered = append(filtered, res.OutboxID)
		}
		if res.SvixAppID != "" {
			appIDs[res.OrganizationID] = res.SvixAppID
		}
	}

	q := repo.New(r.db)
	rows, err := q.FetchOutboxRowsByIDs(ctx, filtered)
	if err != nil {
		return fmt.Errorf("fetch outbox rows by ids: %w", err)
	}

	for _, row := range rows {
		if activity.IsActivity(ctx) {
			activity.RecordHeartbeat(ctx, row.ID)
		}

		svixAppID := appIDs[row.OrganizationID]
		rowErr, infraErr := r.deliverOne(ctx, q, svixAppID, row)
		if infraErr != nil {
			// DB or structural failure — fail the activity so Temporal retries.
			// Svix idempotency keys make retries safe even if the Svix call succeeded.
			return infraErr
		}
		if rowErr != nil {
			r.logger.ErrorContext(ctx, "outbox svix relay delivery failed",
				attr.SlogOrganizationID(row.OrganizationID),
				attr.SlogOutboxID(row.ID),
				attr.SlogOutboxPublicID(row.PublicID.String()),
				attr.SlogSvixAppID(svixAppID),
				attr.SlogError(rowErr),
			)
		}
	}

	return nil
}

// deliverOne publishes a single row and records the outcome.
// rowErr is a per-row Svix-level failure already recorded in the DB — caller
// logs it and moves on. infraErr is a DB or structural failure that prevents
// recording the outcome — caller should fail the activity and let Temporal retry.
func (r *Relay) deliverOne(ctx context.Context, q *repo.Queries, svixAppID string, row repo.FetchOutboxRowsByIDsRow) (rowErr, infraErr error) {
	var payload map[string]any

	// An org without a svix app ID should not have gotten to this point so this
	// is mostly defensive coding against a bug.
	if svixAppID == "" {
		errMsg := "org has no svix_app_id"
		if dbErr := q.MarkOutboxRelayDeadLettered(ctx, repo.MarkOutboxRelayDeadLetteredParams{
			OutboxID:  row.ID,
			LastError: conv.ToPGTextEmpty(errMsg),
		}); dbErr != nil {
			return nil, fmt.Errorf("mark dead-lettered for missing svix_app_id: %w", dbErr)
		}
		return errors.New(errMsg), nil
	}

	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		errMsg := fmt.Sprintf("invalid payload: %v", err)
		// Bad payloads will never become valid; dead-letter immediately.
		if dbErr := q.MarkOutboxRelayDeadLettered(ctx, repo.MarkOutboxRelayDeadLetteredParams{
			OutboxID:  row.ID,
			LastError: conv.ToPGTextEmpty(errMsg),
		}); dbErr != nil {
			return nil, fmt.Errorf("mark dead-lettered after payload error: %w", dbErr)
		}
		return errors.New(errMsg), nil
	}

	publicID := row.PublicID.String()
	out, err := r.svixClient.Message.Create(ctx, svixAppID, models.MessageIn{
		EventId:                &publicID,
		EventType:              row.EventType,
		Payload:                payload,
		Application:            nil,
		Channels:               nil,
		DeliverAt:              nil,
		PayloadRetentionHours:  nil,
		PayloadRetentionPeriod: nil,
		Tags:                   nil,
		TransformationsParams:  nil,
	}, &svix.MessageCreateOptions{
		IdempotencyKey: &publicID,
		WithContent:    nil,
	})
	if err == nil {
		var msgID string
		if out != nil {
			msgID = out.Id
		}
		if dbErr := q.MarkOutboxRelayProcessed(ctx, repo.MarkOutboxRelayProcessedParams{
			OutboxID:      row.ID,
			SvixMessageID: conv.ToPGTextEmpty(msgID),
		}); dbErr != nil {
			return nil, fmt.Errorf("mark processed: %w", dbErr)
		}
		return nil, nil
	}

	wrapped := fmt.Errorf("svix message create: %w", err)
	errMsg := wrapped.Error()
	nextAttempts := row.Attempts + 1
	permanent := isPermanentSvixError(err)
	if permanent || nextAttempts >= r.maxAttempts {
		if dbErr := q.MarkOutboxRelayDeadLettered(ctx, repo.MarkOutboxRelayDeadLetteredParams{
			OutboxID:  row.ID,
			LastError: conv.ToPGTextEmpty(errMsg),
		}); dbErr != nil {
			return nil, fmt.Errorf("mark dead-lettered: %w", dbErr)
		}
		return wrapped, nil
	}

	if dbErr := q.MarkOutboxRelayFailed(ctx, repo.MarkOutboxRelayFailedParams{
		OutboxID:   row.ID,
		LastError:  conv.ToPGTextEmpty(errMsg),
		RetryAfter: calcRetryAfter(row.Attempts),
	}); dbErr != nil {
		return nil, fmt.Errorf("mark failed: %w", dbErr)
	}
	return wrapped, nil
}

// isPermanentSvixError returns true for Svix HTTP responses that will never
// succeed on retry (e.g. 400/403/404). 429 and 5xx are treated as transient.
func isPermanentSvixError(err error) bool {
	var svixErr *svix.Error
	if !errors.As(err, &svixErr) {
		return false
	}
	status := svixErr.Status()
	if status >= 400 && status < 500 && status != 429 {
		return true
	}
	return false
}

func areWebhooksEnabled(
	ctx context.Context,
	client *productfeatures.Client,
	inMemCache map[string]bool,
	res *Event,
) (bool, error) {
	if _, ok := inMemCache[res.OrganizationID]; !ok {
		featureEnabled, err := client.IsFeatureEnabled(ctx, res.OrganizationID, productfeatures.FeatureWebhooks)
		if err != nil {
			return false, fmt.Errorf("check webhooks feature flag: %w", err)
		}

		// 1. check the feature flag for the given org
		// 2. check if the org has onboarded to Svix
		// 3. check that even if they onboarded, they haven't later disabled webhooks
		inMemCache[res.OrganizationID] = featureEnabled && res.SvixAppID != "" && res.WebhooksEnabled
	}

	return inMemCache[res.OrganizationID], nil
}
