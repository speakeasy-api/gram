package chat

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const meterChatDroppedGenerations = "chat.capture.dropped_generations"

// captureMetrics holds the instruments recorded while capturing chat
// completions to the database.
type captureMetrics struct {
	droppedGenerations metric.Int64Counter
}

func newCaptureMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *captureMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/chat")

	droppedGenerations, err := meter.Int64Counter(
		meterChatDroppedGenerations,
		metric.WithDescription("Assistant generations dropped at capture because the model produced malformed tool_call arguments"),
		metric.WithUnit("{generation}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterChatDroppedGenerations), attr.SlogError(err))
	}

	return &captureMetrics{
		droppedGenerations: droppedGenerations,
	}
}

// RecordDroppedGeneration counts an assistant generation dropped at capture
// because the model produced malformed tool_call arguments.
func (m *captureMetrics) RecordDroppedGeneration(ctx context.Context, projectID uuid.UUID, toolName string) {
	if m.droppedGenerations == nil {
		return
	}
	m.droppedGenerations.Add(ctx, 1, metric.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.ToolName(toolName),
	))
}
