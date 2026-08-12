// Package publish_outbox drains the general-purpose publish outbox onto
// Pub/Sub.
//
// The relay is deliberately incurious about what it carries. A row names a
// topic and holds pre-marshaled bytes; the relay claims it, publishes it, and
// deletes it. Nothing here knows what a webhook is, and adding a new kind of
// outbox message requires no change to this package.
//
// One activity backs the whole loop. Claiming, publishing and settling live
// together because splitting them would mean carrying message bodies across a
// Temporal activity boundary, and activity inputs and outputs are recorded in
// workflow history — a 50-row batch of multi-kilobyte messages would bloat the
// history of a workflow that iterates every few seconds. Only counters cross
// the boundary.
//
// Failure handling splits three ways:
//
//   - Permanent failures (oversized payload, undecodable attributes, exhausted
//     retry budget) move to publish_outbox_dead_letters.
//   - Transient failures record an error and a jittered retry_after, releasing
//     the lease so the row is picked up again later.
//   - Database failures fail the activity so Temporal retries the whole batch.
//     Claimed rows are safe: their leases expire and they are re-claimed.
package publish_outbox

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/infra/pkg/topics"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// maxBatchSize is the number of rows claimed per drain iteration.
const maxBatchSize int32 = 50

// maxAttempts caps delivery attempts before a row is dead-lettered. A publish
// failure is almost always infrastructural, so the budget is generous.
const maxAttempts int32 = 10

// claimLease is how long a claimed row stays invisible to other drainers. It
// only has to outlast a publish round trip; if the drainer dies mid-batch, the
// rows become claimable again this soon afterwards.
//
// It is also what bounds a drain: past the lease a batch can no longer be
// settled, because the settlement statements match on the claim that made it.
// The activity deadline in background/publish_outbox.go is set above this on
// purpose, so an attempt is killed only once its claim has certainly lapsed.
const claimLease = 60 * time.Second

// maxMessageBytes mirrors the producer-side guard in the outbox package. Rows
// written before that guard existed, or by a path that bypasses it, would
// otherwise sit at the head of the queue failing forever.
const maxMessageBytes = 9 * 1024 * 1024

const (
	retryBaseDelay = 5 * time.Second
	retryMaxDelay  = 10 * time.Minute
)

const (
	meterPublishedRows    = "publish_outbox.published_rows"
	meterDeadLetteredRows = "publish_outbox.dead_lettered_rows"
	meterPendingRows      = "publish_outbox.pending_rows"
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

// DrainResult reports what one iteration did. HasMore is true when rows remain
// beyond this batch, letting the workflow poll again immediately instead of
// sleeping.
type DrainResult struct {
	Published    int
	DeadLettered int
	Retrying     int
	HasMore      bool
}

type Relay struct {
	logger      *slog.Logger
	tracer      trace.Tracer
	db          *pgxpool.Pool
	publisher   topics.Publisher
	maxAttempts int32

	publishedRows    metric.Int64Counter
	deadLetteredRows metric.Int64Counter
}

func New(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, publisher topics.Publisher) *Relay {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities/publish_outbox")

	publishedRows, err := meter.Int64Counter(
		meterPublishedRows,
		metric.WithDescription("Number of outbox rows successfully published to Pub/Sub"),
		metric.WithUnit("{row}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric error", attr.SlogMetricName(meterPublishedRows), attr.SlogError(err))
	}

	deadLetteredRows, err := meter.Int64Counter(
		meterDeadLetteredRows,
		metric.WithDescription("Number of outbox rows moved to the dead letter table"),
		metric.WithUnit("{row}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric error", attr.SlogMetricName(meterDeadLetteredRows), attr.SlogError(err))
	}

	relay := &Relay{
		logger:           logger,
		tracer:           tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/publish_outbox"),
		db:               db,
		publisher:        publisher,
		maxAttempts:      maxAttempts,
		publishedRows:    publishedRows,
		deadLetteredRows: deadLetteredRows,
	}

	// Queue depth is the signal that matters most here: rows are deleted as they
	// publish, so a non-trivial pending count means the relay is not keeping up
	// or Pub/Sub is refusing writes. The COUNT is only affordable because the
	// table is near-empty in steady state — which is the same property being
	// measured, so a slow observation is itself informative.
	if _, err := meter.Int64ObservableGauge(
		meterPendingRows,
		metric.WithDescription("Number of rows waiting in the publish outbox"),
		metric.WithUnit("{row}"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			n, err := relay.PendingRows(ctx)
			if err != nil {
				return fmt.Errorf("observe pending publish outbox rows: %w", err)
			}
			o.Observe(n)
			return nil
		}),
	); err != nil {
		logger.ErrorContext(context.Background(), "create metric error", attr.SlogMetricName(meterPendingRows), attr.SlogError(err))
	}

	return relay
}

// Drain claims one batch, publishes it, and settles every row.
func (r *Relay) Drain(ctx context.Context) (DrainResult, error) {
	q := repo.New(r.db)

	// Identifies this claim for the rest of the drain. Every settlement carries
	// it, so if the lease expires and another drainer takes the rows over, the
	// statements below match nothing rather than acting on someone else's claim.
	leaseToken, err := uuid.NewV7()
	if err != nil {
		return DrainResult{}, fmt.Errorf("generate publish outbox lease token: %w", err)
	}

	// Claim one extra row to detect whether more remain beyond this batch.
	rows, err := q.ClaimPublishOutboxBatch(ctx, repo.ClaimPublishOutboxBatchParams{
		Lease:      pgtype.Interval{Microseconds: claimLease.Microseconds(), Days: 0, Months: 0, Valid: true},
		LeaseToken: leaseToken,
		BatchSize:  maxBatchSize + 1,
	})
	if err != nil {
		return DrainResult{}, fmt.Errorf("claim publish outbox batch: %w", err)
	}

	hasMore := len(rows) > int(maxBatchSize)
	if hasMore {
		// The surplus row was claimed too. Release it rather than holding a
		// lease we do not intend to act on, so the next iteration sees it
		// immediately instead of waiting out the lease.
		surplus := rows[maxBatchSize:]
		rows = rows[:maxBatchSize]
		if err := r.release(ctx, q, surplus, leaseToken); err != nil {
			return DrainResult{}, err
		}
	}

	trace.SpanFromContext(ctx).SetAttributes(attr.OutboxBatchSize(len(rows)))

	if len(rows) == 0 {
		return DrainResult{Published: 0, DeadLettered: 0, Retrying: 0, HasMore: false}, nil
	}

	results := r.publishAll(ctx, rows)

	return r.settle(ctx, q, rows, results, hasMore, leaseToken)
}

// publishAll issues every publish before waiting on any of them, so the Pub/Sub
// client can batch the whole claim into as few round trips as it likes. Waiting
// on each result in turn would serialise the batch.
func (r *Relay) publishAll(ctx context.Context, rows []repo.ClaimPublishOutboxBatchRow) []error {
	handles := make([]gcp.PublishResult, len(rows))
	failures := make([]error, len(rows))

	for i, row := range rows {
		if len(row.Message) > maxMessageBytes {
			failures[i] = oops.Permanent(fmt.Errorf("message is %d bytes, over the %d byte limit", len(row.Message), maxMessageBytes))
			continue
		}

		attributes, err := unmarshalAttributes(row.Attributes)
		if err != nil {
			failures[i] = oops.Permanent(fmt.Errorf("decode attributes: %w", err))
			continue
		}

		handles[i] = r.publisher.Publish(ctx, row.Topic, row.Message, attributes)
	}

	for i, handle := range handles {
		if handle == nil {
			continue
		}
		if _, err := handle.Get(ctx); err != nil {
			failures[i] = fmt.Errorf("publish to %s: %w", rows[i].Topic, err)
		}
	}

	return failures
}

// settle applies the outcome of a batch: published rows are deleted, permanent
// failures move to the dead letter table, and the rest get a retry window.
func (r *Relay) settle(ctx context.Context, q *repo.Queries, rows []repo.ClaimPublishOutboxBatchRow, failures []error, hasMore bool, leaseToken uuid.UUID) (DrainResult, error) {
	// Nothing about an outcome is shared across the batch, even though each
	// outcome is settled in a single statement. An error names the topic its own
	// row could not reach, and a retry window is only as long as that row's own
	// history warrants — one value for the batch would mislabel most of it.
	var published, deadLetter, retry []int64
	var deadLetterErrs, retryErrs []string
	var retryAfters []pgtype.Timestamptz

	for i, row := range rows {
		err := failures[i]
		switch {
		case err == nil:
			published = append(published, row.ID)
		case isPermanent(err) || row.Attempts >= r.maxAttempts:
			deadLetter = append(deadLetter, row.ID)
			deadLetterErrs = append(deadLetterErrs, errString(err))
			r.logger.ErrorContext(ctx, "publish outbox row dead lettered",
				attr.SlogOrganizationID(row.OrganizationID),
				attr.SlogOutboxID(row.ID),
				attr.SlogOutboxPublicID(row.PublicID.String()),
				attr.SlogTopicProtoName(row.Topic),
				attr.SlogError(err),
			)
		default:
			retry = append(retry, row.ID)
			retryErrs = append(retryErrs, errString(err))
			retryAfters = append(retryAfters, calcRetryAfter(row.Attempts))
			r.logger.WarnContext(ctx, "publish outbox row failed, will retry",
				attr.SlogOrganizationID(row.OrganizationID),
				attr.SlogOutboxID(row.ID),
				attr.SlogOutboxPublicID(row.PublicID.String()),
				attr.SlogTopicProtoName(row.Topic),
				attr.SlogError(err),
			)
		}
	}

	if len(published) > 0 {
		if _, err := q.DeletePublishedOutboxRows(ctx, repo.DeletePublishedOutboxRowsParams{
			Ids:        published,
			LeaseToken: leaseToken,
		}); err != nil {
			return DrainResult{}, fmt.Errorf("delete published outbox rows: %w", err)
		}
		if r.publishedRows != nil {
			r.publishedRows.Add(ctx, int64(len(published)))
		}
	}

	if len(deadLetter) > 0 {
		if _, err := q.DeadLetterPublishOutboxRows(ctx, repo.DeadLetterPublishOutboxRowsParams{
			Ids:        deadLetter,
			Errors:     deadLetterErrs,
			LeaseToken: leaseToken,
		}); err != nil {
			return DrainResult{}, fmt.Errorf("dead letter publish outbox rows: %w", err)
		}
		if r.deadLetteredRows != nil {
			r.deadLetteredRows.Add(ctx, int64(len(deadLetter)))
		}
	}

	if len(retry) > 0 {
		if err := q.MarkPublishOutboxFailed(ctx, repo.MarkPublishOutboxFailedParams{
			Ids:         retry,
			Errors:      retryErrs,
			RetryAfters: retryAfters,
			LeaseToken:  leaseToken,
		}); err != nil {
			return DrainResult{}, fmt.Errorf("mark publish outbox rows failed: %w", err)
		}
	}

	return DrainResult{
		Published:    len(published),
		DeadLettered: len(deadLetter),
		Retrying:     len(retry),
		HasMore:      hasMore,
	}, nil
}

// release clears the lease on rows claimed but not acted upon.
func (r *Relay) release(ctx context.Context, q *repo.Queries, rows []repo.ClaimPublishOutboxBatchRow, leaseToken uuid.UUID) error {
	if len(rows) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	if err := q.ReleasePublishOutboxRows(ctx, repo.ReleasePublishOutboxRowsParams{
		Ids:        ids,
		LeaseToken: leaseToken,
	}); err != nil {
		return fmt.Errorf("release publish outbox rows: %w", err)
	}

	return nil
}

// DeleteDeadLetters bounds the dead letter table, returning the number of rows
// removed so the caller can keep batching until it drains.
func (r *Relay) DeleteDeadLetters(ctx context.Context, cutoff time.Time, batchSize int32) (int64, error) {
	n, err := repo.New(r.db).GCPublishOutboxDeadLetters(ctx, repo.GCPublishOutboxDeadLettersParams{
		Cutoff:    pgtype.Timestamptz{Time: cutoff, InfinityModifier: pgtype.Finite, Valid: true},
		BatchSize: batchSize,
	})
	if err != nil {
		return 0, fmt.Errorf("gc publish outbox dead letters: %w", err)
	}

	return n, nil
}

// PendingRows backs the queue depth gauge.
func (r *Relay) PendingRows(ctx context.Context) (int64, error) {
	n, err := repo.New(r.db).CountPendingPublishOutboxRows(ctx)
	if err != nil {
		return 0, fmt.Errorf("count pending publish outbox rows: %w", err)
	}

	return n, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}
