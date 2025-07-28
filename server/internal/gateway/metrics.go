package gateway

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type metrics struct {
	toolCallsCounter metric.Int64Counter
}

func newMetrics(meter metric.Meter, logger *slog.Logger) *metrics {
	toolCallsCounter, err := meter.Int64Counter(
		"tool.call",
		metric.WithDescription("Number of HTTP tool calls"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create tool calls counter", slog.String("error", err.Error()))
	}

	return &metrics{
		toolCallsCounter: toolCallsCounter,
	}
}

func (m *metrics) RecordHTTPToolCall(ctx context.Context, orgID string, toolName string, statusCode int) {
	if m.toolCallsCounter == nil {
		return
	}

	m.toolCallsCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tool", toolName),
		attribute.String("organization_id", orgID),
		attribute.String("status_code", fmt.Sprintf("%d", statusCode)),
	))
}
