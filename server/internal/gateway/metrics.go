package gateway

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type metrics struct {
	toolCallsCounter metric.Int64Counter
}

// toolCallOutcome classifies how a tool execution ended, on the gram.outcome
// dimension of the tool.call counter. The status code cannot carry this on its
// own: an external MCP server reports failure in-band, as a result flagged
// isError on an otherwise successful HTTP 200 response.
type toolCallOutcome string

const (
	// toolCallOutcomeSuccess is a tool that ran and reported no failure.
	toolCallOutcomeSuccess toolCallOutcome = "success"

	// toolCallOutcomeToolError is a tool that ran and reported a failure,
	// whether through its status code or in-band in its result.
	toolCallOutcomeToolError toolCallOutcome = "tool_error"

	// toolCallOutcomeNoResponse is a tool that produced no response at all: a
	// connection failure, a cancelled call, or an error raised before anything
	// was written back to the caller.
	toolCallOutcomeNoResponse toolCallOutcome = "no_response"
)

// toolCallOutcomeForStatus derives the outcome from the status written back to
// the caller. Zero is the sentinel for a call that never produced a response,
// and is reported separately from a tool that ran and returned a failing
// status.
func toolCallOutcomeForStatus(statusCode int) toolCallOutcome {
	switch {
	case statusCode == 0:
		return toolCallOutcomeNoResponse
	case statusCode >= 400:
		return toolCallOutcomeToolError
	default:
		return toolCallOutcomeSuccess
	}
}

// toolCallRecord describes one completed tool execution for the tool.call
// counter.
type toolCallRecord struct {
	// OrganizationID owns the tool that ran.
	OrganizationID string

	// URN identifies the tool and supplies the tool kind dimension.
	URN urn.Tool

	// ToolName is the name recorded on the tool name dimension. It can differ
	// from the URN name, which for some kinds names the tool's source rather
	// than the tool that ran.
	ToolName string

	// StatusCode is the HTTP status the executor wrote back to the caller, or
	// 0 when it wrote none.
	StatusCode int

	// Outcome classifies the execution for failure-rate reporting.
	Outcome toolCallOutcome
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

func (m *metrics) RecordToolCall(ctx context.Context, rec toolCallRecord) {
	if m.toolCallsCounter == nil {
		return
	}

	kv := []attribute.KeyValue{
		attr.ToolCallKind(string(rec.URN.Kind)),
		attr.ToolName(rec.ToolName),
		attr.OrganizationID(rec.OrganizationID),
		attr.Outcome(rec.Outcome),
		semconv.HTTPResponseStatusCode(rec.StatusCode),
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
		attr.Outcome(toolCallOutcomeForStatus(statusCode)),
		semconv.HTTPResponseStatusCode(statusCode),
	}

	bag := baggage.FromContext(ctx)

	if org := bag.Member(string(attr.OrganizationSlugKey)).Value(); org != "" {
		kv = append(kv, attr.OrganizationSlug(org))
	}

	// for now we will keep it in the general tool call counter, we don't bill differently
	m.toolCallsCounter.Add(ctx, 1, metric.WithAttributes(kv...))
}
