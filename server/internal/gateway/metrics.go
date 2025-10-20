package gateway

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type metrics struct {
	toolCallsCounter metric.Int64Counter
}

func newMetrics(meter metric.Meter, logger *slog.Logger) *metrics {
	toolCallsCounter, err := meter.Int64Counter(
		"tool.call",
		metric.WithDescription("Number of tool calls"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create tool calls counter", attr.SlogError(err))
	}

	return &metrics{
		toolCallsCounter: toolCallsCounter,
	}
}

func (m *metrics) RecordToolCall(ctx context.Context, orgID string, toolURN urn.Tool, statusCode int) {
	if m.toolCallsCounter == nil {
		return
	}

	kv := []attribute.KeyValue{
		attr.ToolCallKind(string(toolURN.Kind)),
		attr.ToolName(toolURN.Name),
		attr.OrganizationID(orgID),
		semconv.HTTPResponseStatusCode(statusCode),
	}

	bag := baggage.FromContext(ctx)

	if org := bag.Member(string(attr.OrganizationSlugKey)).Value(); org != "" {
		kv = append(kv, attr.OrganizationSlug(org))
	}

	m.toolCallsCounter.Add(ctx, 1, metric.WithAttributes(kv...))
}

func (m *metrics) RecordResourceCall(ctx context.Context, orgID string, resourceURN urn.Resource, statusCode int) {
	if m.toolCallsCounter == nil {
		return
	}

	kv := []attribute.KeyValue{
		attr.ResourceURN(resourceURN.String()),
		attr.OrganizationID(orgID),
		semconv.HTTPResponseStatusCode(statusCode),
	}

	bag := baggage.FromContext(ctx)

	if org := bag.Member(string(attr.OrganizationSlugKey)).Value(); org != "" {
		kv = append(kv, attr.OrganizationSlug(org))
	}

	// for now we will keep it in the general tool call counter, we don't bill differently
	m.toolCallsCounter.Add(ctx, 1, metric.WithAttributes(kv...))
}
