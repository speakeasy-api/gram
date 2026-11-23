package chat

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterTurnStreamFrameBytes        = "chat.turn_stream.frame_bytes"
	meterTurnStreamOmittedBodyBytes  = "chat.turn_stream.omitted_body_bytes"
	turnStreamMetricsInstrumentation = "github.com/speakeasy-api/gram/server/internal/chat"
)

// turnStreamByteBuckets spans a streamed text delta (tens of bytes) up past
// the per-frame cap to the multi-megabyte tool outputs that are left out, so
// both histograms show where the cap sits in the distribution.
var turnStreamByteBuckets = []float64{
	64,
	256,
	1 << 10, // 1 KiB
	4 << 10, // 4 KiB
	turnFrameMaxBodyBytes,
	16 << 10,  // 16 KiB
	64 << 10,  // 64 KiB
	256 << 10, // 256 KiB
	1 << 20,   // 1 MiB
	4 << 20,   // 4 MiB
}

// turnStreamMetrics records the bytes turn frames put into Redis, so the
// per-frame cap can be tuned against what is actually published.
type turnStreamMetrics struct {
	// frameBytes is the encoded size of each appended frame.
	frameBytes metric.Int64Histogram

	// omittedBodyBytes is the size of each body left out of a frame for
	// being over the cap.
	omittedBodyBytes metric.Int64Histogram
}

var (
	turnStreamMetricsOnce   sync.Once
	turnStreamMetricsShared *turnStreamMetrics
)

// loadTurnStreamMetrics returns instruments on the global meter provider.
// TurnStream is built where only a Redis client is to hand, and the global
// provider delegates to the configured one once it is installed.
func loadTurnStreamMetrics() *turnStreamMetrics {
	turnStreamMetricsOnce.Do(func() {
		turnStreamMetricsShared = newTurnStreamMetrics(otel.GetMeterProvider())
	})
	return turnStreamMetricsShared
}

// newTurnStreamMetrics creates the instruments. An instrument that fails to
// register is left nil and its recordings are dropped: losing a metric must
// not stop frames being published.
func newTurnStreamMetrics(meterProvider metric.MeterProvider) *turnStreamMetrics {
	meter := meterProvider.Meter(turnStreamMetricsInstrumentation)
	m := &turnStreamMetrics{frameBytes: nil, omittedBodyBytes: nil}
	if h, err := meter.Int64Histogram(meterTurnStreamFrameBytes,
		metric.WithDescription("Encoded bytes of each frame appended to a chat's turn stream in Redis"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(turnStreamByteBuckets...),
	); err == nil {
		m.frameBytes = h
	}
	if h, err := meter.Int64Histogram(meterTurnStreamOmittedBodyBytes,
		metric.WithDescription("Bytes of a tool output or tool-call JSON left out of a turn frame for exceeding the per-frame cap"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(turnStreamByteBuckets...),
	); err == nil {
		m.omittedBodyBytes = h
	}
	return m
}

// recordAppend records one appended frame's encoded size.
func (m *turnStreamMetrics) recordAppend(ctx context.Context, frame TurnFrame, payloadBytes int) {
	if m == nil || m.frameBytes == nil {
		return
	}
	m.frameBytes.Record(ctx, int64(payloadBytes), metric.WithAttributes(
		attr.ChatTurnFrameKind(string(frame.Kind)),
		attr.ChatTurnFrameBodyOmitted(frame.BodyOmitted),
	))
}

// recordOmitted records the size of a body left out of a frame.
func (m *turnStreamMetrics) recordOmitted(ctx context.Context, kind TurnFrameKind, bodyBytes int) {
	if m == nil || m.omittedBodyBytes == nil {
		return
	}
	m.omittedBodyBytes.Record(ctx, int64(bodyBytes), metric.WithAttributes(
		attr.ChatTurnFrameKind(string(kind)),
	))
}
