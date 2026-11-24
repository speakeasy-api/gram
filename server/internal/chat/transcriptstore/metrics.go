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
)

// captureDurationBuckets are in seconds. A KMS unwrap takes about 150 to 250
// ms in prod, so the buckets split that range into 50 ms steps and stretch to
// the slow tail that would hold a message transaction open.
var captureDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.5, 1, 2.5, 5}

// captureOutcome classifies a Capture call for metrics and spans. Any outcome
// other than success rolls back the caller's message write. Invalid arguments
// return ErrInvalid before telemetry starts, so they have no outcome.
type captureOutcome string

const (
	captureOutcomeSuccess  captureOutcome = "success"
	captureOutcomeNotFound captureOutcome = "not_found"
	captureOutcomeChanged  captureOutcome = "changed"
	captureOutcomeError    captureOutcome = "error"
)

func classifyCapture(err error) captureOutcome {
	switch {
	case err == nil:
		return captureOutcomeSuccess
	case errors.Is(err, ErrNotFound):
		return captureOutcomeNotFound
	case errors.Is(err, ErrChanged):
		return captureOutcomeChanged
	default:
		return captureOutcomeError
	}
}

type captureMetrics struct {
	duration metric.Float64Histogram
	messages metric.Int64Counter
}

func newCaptureMetrics(meterProvider metric.MeterProvider) (*captureMetrics, error) {
	meter := meterProvider.Meter(instrumentationName)

	duration, err := meter.Float64Histogram(
		meterCaptureDuration,
		metric.WithDescription("Time to seal and store encrypted transcript shadows for one message write"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(captureDurationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", meterCaptureDuration, err)
	}

	messages, err := meter.Int64Counter(
		meterCaptureMessages,
		metric.WithDescription("Chat messages submitted for encrypted transcript shadow capture"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", meterCaptureMessages, err)
	}

	return &captureMetrics{duration: duration, messages: messages}, nil
}

// record is keyed by project because capture only runs for an explicit,
// short project allowlist, so the series count stays small.
func (m *captureMetrics) record(ctx context.Context, projectID uuid.UUID, messages int, elapsed time.Duration, outcome captureOutcome) {
	opts := metric.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.Outcome(outcome),
	)
	m.duration.Record(ctx, elapsed.Seconds(), opts)
	m.messages.Add(ctx, int64(messages), opts)
}
