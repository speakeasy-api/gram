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
	// conversationOutcomeAmbiguousPrefix: more than one of the actor's chats
	// ends in the transcript's longest stored prefix, so none can be chosen and
	// the frame starts a new chat like conversationOutcomeNew.
	conversationOutcomeAmbiguousPrefix = "ambiguous_prefix"
	conversationOutcomeNew             = "new"
)

// metrics counts how inference frames map onto stored conversations.
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
	m.conversations.Add(ctx, 1, metric.WithAttributes(
		attr.InferenceApplication(metricApplication(frame.Source.Application)),
		attr.InferenceHasSessionID(frame.SessionID != ""),
		attr.InferenceConversationOutcome(outcome),
	))
}

// metricApplication folds the advisory source.application into a bounded
// label: a product surface productSource knows, or "other". The raw value is
// client-controlled and would otherwise mint a metric series per distinct
// string; it still reaches logs and stored messages unchanged.
func metricApplication(application string) string {
	if source, known := productSource(application); known {
		return source
	}
	return "other"
}
