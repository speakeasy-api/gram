package authz

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/inv"
)

const (
	// publishAckAwaitTimeout bounds the detached goroutine that drains the
	// publish ack for one challenge row. The broker publish itself is bounded
	// separately by PublishSettings.Timeout, configured where the authz
	// challenge publisher is constructed.
	publishAckAwaitTimeout = 10 * time.Second
)

// NewNoopChallengePublisher returns an inert ChallengePublisher for tests and
// processes that do not publish authz challenges.
func NewNoopChallengePublisher(logger *slog.Logger) *ChallengePublisher {
	return NewChallengePublisher(
		logger,
		tracenoop.NewTracerProvider(),
		gcp.NewNoopPublisher[*authzv1.ChallengeRow](),
	)
}

// ChallengePublisher sends authz challenge rows to the
// gram-authz-v1-challenge-row Pub/Sub topic. The streams ChallengeCHWriter
// consumer inserts them into ClickHouse — keeping the CH pool off the authz
// request path.
type ChallengePublisher struct {
	logger *slog.Logger
	tracer trace.Tracer
	pub    gcp.Publisher[*authzv1.ChallengeRow]

	// drains tracks in-flight ack-drain goroutines so tests can await them
	// deterministically (see WaitForPublishDrains in export_test.go).
	drains sync.WaitGroup
}

// NewChallengePublisher constructs a ChallengePublisher. Callers must always
// pass a publisher — a real Pub/Sub publisher, gcp.NewNoopPublisher where the
// write is not wanted, or a mock/direct inserter in tests.
func NewChallengePublisher(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	pub gcp.Publisher[*authzv1.ChallengeRow],
) *ChallengePublisher {
	inv.Require(
		"authz challenge publisher",
		"publisher set", pub != nil,
	)

	return &ChallengePublisher{
		logger: logger.With(attr.SlogComponent("authz_challenge_publisher")),
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/authz"),
		pub:    pub,
		drains: sync.WaitGroup{},
	}
}

// PublishChallenge publishes one challenge row to Pub/Sub. It is best-effort
// and non-blocking: it never blocks on broker acks (results are drained on a
// detached goroutine) and must never stall the authz request path.
func (p *ChallengePublisher) PublishChallenge(ctx context.Context, row authzrepo.ChallengeRow) {
	// Caller cancellation (request teardown) must not abort the publish: a
	// row skipped here is never re-emitted. Detach cancellation while keeping
	// trace context; PublishSettings.Timeout and publishAckAwaitTimeout bound
	// the work instead.
	ctx = context.WithoutCancel(ctx)

	ctx, span := p.tracer.Start(ctx, "authz.publishChallenge")
	defer span.End()

	result := p.pub.Publish(ctx, challengeRowToProto(row))

	p.drains.Add(1)
	go p.drainPublishAck(ctx, result)
}

// drainPublishAck waits for one publish result and surfaces failures.
func (p *ChallengePublisher) drainPublishAck(ctx context.Context, result gcp.PublishResult) {
	defer p.drains.Done()

	ctx, cancel := context.WithTimeout(ctx, publishAckAwaitTimeout)
	defer cancel()

	if _, err := result.Get(ctx); err != nil {
		p.logger.ErrorContext(ctx, "failed to publish authz challenge to pubsub",
			attr.SlogError(err),
		)
	}
}
