package transcriptstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	instrumentationName = "github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"

	// meterCaptureDuration is the time one Capture call spends inside the
	// caller's message transaction, including the KMS unwrap of the
	// organization's data key that every batch performs.
	meterCaptureDuration = "chat.transcript_shadow.capture.duration"

	// meterCaptureMessages counts the messages each Capture call was asked to
	// seal, by outcome.
	meterCaptureMessages = "chat.transcript_shadow.capture.messages"

	// meterReadDuration is the time one LoadMany call spends loading, opening
	// and verifying the transcripts for a page of messages.
	meterReadDuration = "chat.transcript_shadow.read.duration"

	// meterReadMessages counts the messages each LoadMany call was asked to
	// open, by outcome.
	meterReadMessages = "chat.transcript_shadow.read.messages"

	// meterUpdateDuration is the time one shadow-mode UpdateChatMessage call spends
	// inside the caller's transaction.
	meterUpdateDuration = "chat.transcript_shadow.update.duration"

	// meterUpdateMessages counts shadow-mode UpdateChatMessage calls by outcome.
	meterUpdateMessages = "chat.transcript_shadow.update.messages"

	// meterUpdateChecks counts one consistency result per shadow-mode
	// UpdateChatMessage call: success when both copies agreed before and after (or
	// before, for an update that changed nothing), otherwise the check that
	// failed, or error when reading or decrypting the transcript failed and the
	// update rolled back. It tracks how well shadow mode keeps the transcript and
	// the plaintext in step.
	meterUpdateChecks = "chat.transcript_shadow.update.checks"
)

// durationBuckets are in seconds. A KMS unwrap takes about 150 to 250 ms in
// prod, so the buckets split that range into 50 ms steps and stretch to the
// slow tail that would hold a message transaction or a chat page open.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.5, 1, 2.5, 5}

// outcome classifies a Capture or LoadMany call for metrics and spans. Any
// capture outcome other than success rolls back the caller's message write.
// On reads, success and plaintext_fallback both serve the message; every
// other read outcome fails the caller's read.
type outcome string

const (
	outcomeSuccess outcome = "success"

	// outcomePlaintextFallback counts read messages that have no transcript at
	// all, which the caller serves from plaintext. It is a per-message
	// outcome; the LoadMany call itself still succeeds.
	outcomePlaintextFallback outcome = "plaintext_fallback"

	// Consistency checks of a shadow-mode chat message update. None of these fails the
	// update: the plaintext row wins and the error is logged. A read or decrypt
	// failure does fail it and is counted as error.
	//
	// outcomeMissingTranscript: the message had no current transcript to
	// start from (none yet, or only a legacy one).
	outcomeMissingTranscript outcome = "missing_transcript"
	// outcomeHydrateMismatch: the decrypted transcript disagreed with the
	// plaintext row before the change, so it was discarded.
	outcomeHydrateMismatch outcome = "hydrate_mismatch"
	// outcomeSanityMismatch: after the write, the stored transcript did not
	// decrypt to the stored plaintext row.
	outcomeSanityMismatch outcome = "sanity_mismatch"

	outcomeNotFound outcome = "not_found"
	outcomeStale    outcome = "stale"
	outcomeInvalid  outcome = "invalid"
	outcomeError    outcome = "error"
)

func classify(err error) outcome {
	switch {
	case err == nil:
		return outcomeSuccess
	case errors.Is(err, ErrNotFound):
		return outcomeNotFound
	case errors.Is(err, ErrStale):
		return outcomeStale
	case errors.Is(err, ErrInvalid):
		return outcomeInvalid
	default:
		return outcomeError
	}
}

type storeMetrics struct {
	capture operationMetrics
	read    operationMetrics
	update  operationMetrics
	checks  metric.Int64Counter
}

type operationMetrics struct {
	duration metric.Float64Histogram
	messages metric.Int64Counter
}

func newStoreMetrics(meterProvider metric.MeterProvider) (*storeMetrics, error) {
	meter := meterProvider.Meter(instrumentationName)

	capture, err := newOperationMetrics(meter, meterCaptureDuration, meterCaptureMessages,
		"Time to seal and store encrypted transcripts for one message write",
		"Chat messages submitted for encrypted transcript capture")
	if err != nil {
		return nil, err
	}

	read, err := newOperationMetrics(meter, meterReadDuration, meterReadMessages,
		"Time to load, open and verify encrypted transcripts for one page of messages",
		"Chat messages requested from encrypted transcripts")
	if err != nil {
		return nil, err
	}

	update, err := newOperationMetrics(meter, meterUpdateDuration, meterUpdateMessages,
		"Time to update a chat message and its encrypted transcript in one transaction",
		"Chat message updates in shadow mode")
	if err != nil {
		return nil, err
	}

	checks, err := meter.Int64Counter(
		meterUpdateChecks,
		metric.WithDescription("Consistency checks of shadow-mode chat message updates, by result"),
		metric.WithUnit("{check}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", meterUpdateChecks, err)
	}

	return &storeMetrics{capture: capture, read: read, update: update, checks: checks}, nil
}

func newOperationMetrics(meter metric.Meter, durationName, messagesName, durationDescription, messagesDescription string) (operationMetrics, error) {

	duration, err := meter.Float64Histogram(
		durationName,
		metric.WithDescription(durationDescription),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	if err != nil {
		return operationMetrics{}, fmt.Errorf("create %s: %w", durationName, err)
	}

	messages, err := meter.Int64Counter(
		messagesName,
		metric.WithDescription(messagesDescription),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return operationMetrics{}, fmt.Errorf("create %s: %w", messagesName, err)
	}

	return operationMetrics{duration: duration, messages: messages}, nil
}

// record is keyed by project because transcripts only exist for an explicit,
// short project allowlist, so the series count stays small.
func (m operationMetrics) record(ctx context.Context, projectID uuid.UUID, messages int, elapsed time.Duration, result outcome) {
	opts := metric.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.Outcome(result),
	)
	m.duration.Record(ctx, elapsed.Seconds(), opts)
	m.messages.Add(ctx, int64(messages), opts)
}

// recordMessages adds a message count for one outcome without a duration, for
// the per-message split of a call whose duration is already recorded.
func (m operationMetrics) recordMessages(ctx context.Context, projectID uuid.UUID, messages int, result outcome) {
	if messages == 0 {
		return
	}
	m.messages.Add(ctx, int64(messages), metric.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.Outcome(result),
	))
}

// recordDuration records one call's duration by outcome.
func (m operationMetrics) recordDuration(ctx context.Context, projectID uuid.UUID, elapsed time.Duration, result outcome) {
	m.duration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.Outcome(result),
	))
}

// check counts one consistency check result of a shadow-mode chat message update.
func (m *storeMetrics) check(ctx context.Context, projectID uuid.UUID, result outcome) {
	m.checks.Add(ctx, 1, metric.WithAttributes(attr.ProjectID(projectID.String()), attr.Outcome(result)))
}
