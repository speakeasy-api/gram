package platformmcp

import (
	"context"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	toolResultSizeMetric   = "platform_mcp.tool.result.size"
	toolCallDurationMetric = "platform_mcp.tool.duration"
)

// Outcomes of one tool call, recorded with its duration.
const (
	toolCallOutcomeOK      = "ok"
	toolCallOutcomeRefused = "refused"
	toolCallOutcomeError   = "error"
)

type toolCallInstruments struct {
	resultSize metric.Int64Histogram
	duration   metric.Float64Histogram
}

var (
	toolCallInstrumentsOnce sync.Once
	toolCallMeters          toolCallInstruments
)

// loadToolCallInstruments builds the histograms once. Size buckets double from
// 1 KiB to 1 MiB so the result budgets can be tuned from what tools return.
func loadToolCallInstruments() toolCallInstruments {
	toolCallInstrumentsOnce.Do(func() {
		meter := otel.GetMeterProvider().Meter("github.com/speakeasy-api/gram/server/internal/platformmcp")
		if h, err := meter.Int64Histogram(
			toolResultSizeMetric,
			metric.WithDescription("Serialized size of one Platform MCP tool result, by tool and audience."),
			metric.WithUnit("By"),
			metric.WithExplicitBucketBoundaries(1<<10, 2<<10, 4<<10, 8<<10, 16<<10, 32<<10, 64<<10, 128<<10, 256<<10, 1<<20),
		); err == nil {
			toolCallMeters.resultSize = h
		}
		if h, err := meter.Float64Histogram(
			toolCallDurationMetric,
			metric.WithDescription("Time one Platform MCP tool call takes to answer, by tool, audience and outcome."),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
		); err == nil {
			toolCallMeters.duration = h
		}
	})
	return toolCallMeters
}

// recordToolResultSize records one successful result's size. Tool names come
// from the registered catalogue, so the attribute set stays bounded.
func recordToolResultSize(ctx context.Context, toolName string, audience Audience, bytes int, refused bool) {
	if h := loadToolCallInstruments().resultSize; h != nil {
		h.Record(ctx, int64(bytes), metric.WithAttributes(
			attribute.String("platform_mcp.tool", toolName),
			attribute.String("platform_mcp.audience", string(audience)),
			attribute.Bool("platform_mcp.result.refused", refused),
		))
	}
}

// recordToolCallDuration records how long one call took to answer.
func recordToolCallDuration(ctx context.Context, toolName string, audience Audience, outcome string, elapsed time.Duration) {
	if h := loadToolCallInstruments().duration; h != nil {
		h.Record(ctx, elapsed.Seconds(), metric.WithAttributes(
			attribute.String("platform_mcp.tool", toolName),
			attribute.String("platform_mcp.audience", string(audience)),
			attribute.String("platform_mcp.outcome", outcome),
		))
	}
}

// callOutcome names how a call ended: an error, a refusal the model reads,
// or a result.
func callOutcome(result *mcp.CallToolResult, err error) string {
	switch {
	case err != nil:
		return toolCallOutcomeError
	case result != nil && result.IsError:
		return toolCallOutcomeRefused
	default:
		return toolCallOutcomeOK
	}
}
