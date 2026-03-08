package mcp

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type metrics struct {
	mcpToolCallCounter    metric.Int64Counter
	mcpRequestDuration    metric.Float64Histogram
	rateLimitCheckCounter metric.Int64Counter
}

func newMetrics(meter metric.Meter, logger *slog.Logger) *metrics {
	mcpToolCallCounter, err := meter.Int64Counter(
		"mcp.tool.call",
		metric.WithDescription("MCP tool call"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create mcp tool call counter", attr.SlogError(err))
	}

	mcpRequestDuration, err := meter.Float64Histogram(
		"mcp.request.duration",
		metric.WithDescription("Duration of mcp request in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 240),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create mcp request duration", attr.SlogError(err))
	}

	rateLimitCheckCounter, err := meter.Int64Counter(
		"mcp.ratelimit.check",
		metric.WithDescription("Rate limit checks on MCP requests"),
		metric.WithUnit("{check}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create rate limit check counter", attr.SlogError(err))
	}

	return &metrics{
		mcpToolCallCounter:    mcpToolCallCounter,
		mcpRequestDuration:    mcpRequestDuration,
		rateLimitCheckCounter: rateLimitCheckCounter,
	}
}

func (m *metrics) RecordMCPToolCall(ctx context.Context, orgID string, mcpURL string, toolName string) {
	if m.mcpToolCallCounter == nil {
		return
	}

	kv := []attribute.KeyValue{
		attr.McpURL(mcpURL),
		attr.ToolName(toolName),
		attr.OrganizationID(orgID),
	}
	m.mcpToolCallCounter.Add(ctx, 1, metric.WithAttributes(kv...))
}

func (m *metrics) RecordRateLimitCheck(ctx context.Context, layer string, key string, allowed bool) {
	if m.rateLimitCheckCounter == nil {
		return
	}

	outcome := "allowed"
	if !allowed {
		outcome = "limited"
	}

	kv := []attribute.KeyValue{
		attr.RateLimitLayer(layer),
		attr.RateLimitKey(key),
		attr.Outcome(outcome),
	}
	m.rateLimitCheckCounter.Add(ctx, 1, metric.WithAttributes(kv...))
}

func (m *metrics) RecordMCPRequestDuration(ctx context.Context, mcpMethod string, mcpURL string, duration time.Duration) {
	if m.mcpRequestDuration == nil {
		return
	}

	kv := []attribute.KeyValue{
		attr.McpMethod(mcpMethod),
		attr.McpURL(mcpURL),
	}

	m.mcpRequestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(kv...))
}
