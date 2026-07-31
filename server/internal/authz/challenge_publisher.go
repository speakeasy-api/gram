package authz

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/pubsub"
)

// NewNoopChallengePublisher returns an inert ChallengePublisher for tests and
// processes that do not publish authz challenges.
func NewNoopChallengePublisher(logger *slog.Logger) *ChallengePublisher {
	return NewChallengePublisher(
		logger,
		tracenoop.NewTracerProvider(),
		metricnoop.NewMeterProvider(),
		gcp.NewNoopPublisher[*authzv1.ChallengeRow](),
	)
}

// ChallengePublisher sends authz challenge rows to the
// gram-authz-v1-challenge-row Pub/Sub topic. The streams ChallengeCHWriter
// consumer inserts them into ClickHouse — keeping the CH pool off the authz
// request path.
type ChallengePublisher struct {
	tracer  trace.Tracer
	pub     gcp.Publisher[*authzv1.ChallengeRow]
	drainer *pubsub.Drainer
}

// NewChallengePublisher constructs a ChallengePublisher. Callers must always
// pass a publisher — a real Pub/Sub publisher, gcp.NewNoopPublisher where the
// write is not wanted, or a mock/direct inserter in tests.
func NewChallengePublisher(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	pub gcp.Publisher[*authzv1.ChallengeRow],
) *ChallengePublisher {
	inv.Require(
		"authz challenge publisher",
		"publisher set", pub != nil,
	)

	return &ChallengePublisher{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/authz"),
		pub:    pub,
		// The drainer stamps the component attribute on its own logger, so it
		// takes the raw one.
		drainer: pubsub.NewDrainer(
			logger,
			meterProvider,
			"authz_challenge_publisher",
			"failed to publish authz challenge to pubsub",
		),
	}
}

// PublishChallenge publishes one challenge row to Pub/Sub. It is best-effort
// and non-blocking: it never waits on broker acks (results are handed to a
// bounded drain pool) and must never stall the authz request path.
func (p *ChallengePublisher) PublishChallenge(ctx context.Context, row authzrepo.ChallengeRow) {
	// Caller cancellation (request teardown) must not abort the publish: a
	// row skipped here is never re-emitted. Detach cancellation while keeping
	// trace context; PublishSettings.Timeout bounds the work instead.
	ctx = context.WithoutCancel(ctx)

	ctx, span := p.tracer.Start(ctx, "authz.publishChallenge")
	defer span.End()

	// The span covers the enqueue only. The ack lands after it closes, and is
	// reported on the pubsub.publish.ack_* counters instead.
	p.drainer.Enqueue(ctx, p.pub.Publish(ctx, challengeRowToProto(row)))
}

// Close releases the ack drain pool, waiting for queued publishes to resolve
// until ctx expires.
func (p *ChallengePublisher) Close(ctx context.Context) error {
	if err := p.drainer.Close(ctx); err != nil {
		return fmt.Errorf("close authz challenge publisher: %w", err)
	}

	return nil
}
