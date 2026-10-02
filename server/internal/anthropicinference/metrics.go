package anthropicinference

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterConversations = "anthropic_inference.conversations"

	// Conversation resolution outcomes recorded on meterConversations and the
	// resolution log line.
	conversationOutcomeSession       = "session"
	conversationOutcomeAdoptedPrefix = "adopted_prefix"
	conversationOutcomeNew           = "new"
)

// metrics counts how inference frames map onto stored conversations. A nil
// *metrics records nothing, so tests can build a Service without a meter.
type metrics struct {
	conversations metric.Int64Counter
}

func newMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *metrics {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/anthropicinference")
	conversations, err := meter.Int64Counter(
		meterConversations,
		metric.WithDescription("Anthropic inference frames by how they resolved to a stored conversation: by session id, by adopting the chat holding the transcript prefix, or as a new chat"),
		metric.WithUnit("{frame}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterConversations), attr.SlogError(err))
	}
	return &metrics{conversations: conversations}
}

// RecordConversation records one frame's resolution outcome.
func (m *metrics) RecordConversation(ctx context.Context, frame Frame, outcome string) {
	if m == nil || m.conversations == nil {
		return
	}
	m.conversations.Add(ctx, 1, metric.WithAttributes(
		attr.InferenceApplication(inferenceSource(frame.Source.Application)),
		attr.InferenceHasSessionID(frame.SessionID != ""),
		attr.InferenceConversationOutcome(outcome),
	))
}
