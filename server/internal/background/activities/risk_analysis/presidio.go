package risk_analysis

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// SourcePresidio is the source label written on every risk_results row
// produced by the Presidio path, including dead-letter sentinels.
const SourcePresidio = "presidio"

// DeadLetterRuleID is set on the synthetic Finding emitted by the retry
// orchestrator when a message permanently fails analysis. buildRows uses
// it as the rule_id for the dead-letter row.
const DeadLetterRuleID = "presidio.dead_letter"

const (
	// retryingMaxAttempts caps how many times the orchestrator hands the same
	// text back to the PresidioClient before giving up and dead-lettering. The
	// client itself does no retry, so this is the only retry layer.
	retryingMaxAttempts = 3

	// retryingBaseBackoff is the initial inter-attempt sleep. Subsequent
	// attempts use full-jittered exponential backoff up to retryingMaxBackoff.
	retryingBaseBackoff = 100 * time.Millisecond

	// retryingMaxBackoff caps the per-attempt jittered backoff so a stalled
	// upstream cannot drag the whole batch past the activity heartbeat.
	retryingMaxBackoff = 1 * time.Second
)

// PIIScanner detects personally identifiable information in text.
type PIIScanner interface {
	// AnalyzeBatch sends multiple texts to the PII analyzer and returns
	// findings for each. The outer slice is indexed by input position.
	// When entities is non-empty, only those entity types are detected.
	//
	// Implementations may return partial results alongside a non-nil error
	// when some inputs were analyzed successfully but others failed —
	// callers MUST consume results regardless of error so successful
	// findings are not discarded. Permanent per-message failures surface
	// as a single Finding with DeadLetterReason populated rather than as
	// an error.
	AnalyzeBatch(ctx context.Context, texts []string, entities []string, onProgress func()) ([][]Finding, error)
}

// StubPIIScanner is a no-op implementation for environments without Presidio.
type StubPIIScanner struct{}

func (s *StubPIIScanner) AnalyzeBatch(_ context.Context, texts []string, _ []string, _ func()) ([][]Finding, error) {
	return make([][]Finding, len(texts)), nil
}

// RetryingPIIScanner is the production PIIScanner implementation. It owns the
// per-message fan-out (one goroutine per text), the retry budget, and the
// dead-letter sentinel that fires when the budget is exhausted. The wrapped
// PresidioClient performs exactly one HTTP attempt per call; all retry
// behaviour lives here.
type RetryingPIIScanner struct {
	client                *PresidioClient
	tracer                trace.Tracer
	logger                *slog.Logger
	maxAttempts           int
	baseBackoff           time.Duration
	deadLetterCounter     metric.Int64Counter
	attemptFailureCounter metric.Int64Counter
}

// NewRetryingPIIScanner wraps a PresidioClient with the per-text retry and
// dead-letter orchestration. The client must be non-nil; for environments
// without Presidio use StubPIIScanner instead.
func NewRetryingPIIScanner(client *PresidioClient, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, logger *slog.Logger) *RetryingPIIScanner {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/presidio")

	deadLetterCounter, _ := meter.Int64Counter(
		"risk.presidio.dead_letters",
		metric.WithDescription("Number of messages dead-lettered after exhausting the Presidio retry budget"),
		metric.WithUnit("{message}"),
	)

	attemptFailureCounter, _ := meter.Int64Counter(
		"risk.presidio.attempt_failures",
		metric.WithDescription("Number of per-message Presidio attempts that failed before retry or dead-letter"),
		metric.WithUnit("{attempt}"),
	)

	return &RetryingPIIScanner{
		client:                client,
		tracer:                tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis/presidio"),
		logger:                logger,
		maxAttempts:           retryingMaxAttempts,
		baseBackoff:           retryingBaseBackoff,
		deadLetterCounter:     deadLetterCounter,
		attemptFailureCounter: attemptFailureCounter,
	}
}

// AnalyzeBatch fans the input texts out to per-text goroutines and waits for
// all of them to settle. Each goroutine retries the PresidioClient up to
// maxAttempts times with jittered exponential backoff. Texts that exhaust the
// budget are returned as a single Finding with DeadLetterReason populated so
// the caller (analyze_batch.buildRows) can persist a dead-letter row instead
// of silently dropping the message.
//
// The returned error is non-nil only on ctx cancellation; per-message
// failures surface as DeadLetterReason on the corresponding Finding so the
// activity can still write results for the rest of the batch.
func (r *RetryingPIIScanner) AnalyzeBatch(ctx context.Context, texts []string, entities []string, onProgress func()) (_ [][]Finding, err error) {
	n := len(texts)
	if n == 0 {
		return nil, nil
	}

	ctx, span := r.tracer.Start(ctx, "presidio.retryingAnalyzeBatch", trace.WithAttributes(
		attribute.Int("presidio.batch_size", n),
		attribute.Int("presidio.max_attempts", r.maxAttempts),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	results := make([][]Finding, n)
	var deadLetters atomic.Int64

	var wg sync.WaitGroup
	for i, text := range texts {
		wg.Go(func() {
			finding, dl := r.analyzeOne(ctx, i, text, entities, onProgress)
			results[i] = finding
			if dl {
				deadLetters.Add(1)
			}
		})
	}
	wg.Wait()

	span.SetAttributes(attribute.Int("presidio.dead_letters", int(deadLetters.Load())))

	if ctx.Err() != nil {
		return results, fmt.Errorf("presidio retrying scan canceled: %w", ctx.Err())
	}
	return results, nil
}

// analyzeOne runs the retry loop for a single text. Returns the per-text
// findings slice (real findings, or a single dead-letter sentinel) and a
// boolean indicating whether the result was a dead letter.
func (r *RetryingPIIScanner) analyzeOne(ctx context.Context, idx int, text string, entities []string, onProgress func()) ([]Finding, bool) {
	if onProgress != nil {
		onProgress()
	}

	var lastErr error
	for attempt := 1; attempt <= r.maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, false
		}

		batch, err := r.client.AnalyzeBatch(ctx, []string{text}, entities, onProgress)
		if err == nil {
			if len(batch) > 0 {
				return batch[0], false
			}
			return nil, false
		}

		lastErr = err
		if r.attemptFailureCounter != nil {
			r.attemptFailureCounter.Add(ctx, 1)
		}

		// Bail only when the outer ctx is cancelled — inner per-request
		// timeouts (analyzeRequestTimeout) and other transient errors
		// should consume retry budget instead.
		if ctx.Err() != nil {
			return nil, false
		}

		if attempt == r.maxAttempts {
			break
		}

		r.logger.WarnContext(ctx, "presidio analyze attempt failed, retrying",
			attr.SlogError(err),
			attr.SlogRiskScanAttempt(attempt),
			attr.SlogRiskScanMaxAttempts(r.maxAttempts),
			attr.SlogRiskScanTextSize(len(text)),
		)

		if !sleepCtx(ctx, computeRetryBackoff(r.baseBackoff, attempt-1)) {
			return nil, false
		}
	}

	r.logger.WarnContext(ctx, "presidio dead-letter: exhausted retry budget",
		attr.SlogError(lastErr),
		attr.SlogRiskScanMaxAttempts(r.maxAttempts),
		attr.SlogRiskScanTextSize(len(text)),
		attr.SlogRiskScanBatchIndex(idx),
	)
	if r.deadLetterCounter != nil {
		r.deadLetterCounter.Add(ctx, 1)
	}

	return []Finding{{
		Source:           SourcePresidio,
		RuleID:           DeadLetterRuleID,
		Description:      "presidio could not analyze message after exhausting retry budget",
		Match:            "",
		StartPos:         0,
		EndPos:           0,
		Tags:             nil,
		Confidence:       0,
		DeadLetterReason: lastErr.Error(),
	}}, true
}

// computeRetryBackoff returns a full-jittered exponential backoff for the
// given attempt index (0-based): uniform in [0, min(cap, base*2^attempt)).
// Returns 0 when base is 0 so tests can disable the wait.
func computeRetryBackoff(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		return 0
	}
	backoff := base
	for range attempt {
		backoff *= 2
		if backoff >= retryingMaxBackoff {
			backoff = retryingMaxBackoff
			break
		}
	}
	return time.Duration(rand.Int64N(int64(backoff))) // #nosec G404 -- jitter, not security-sensitive
}
