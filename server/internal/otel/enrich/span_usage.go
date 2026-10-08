package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// spanUsage is logUsage for spans.
type spanUsage struct {
	instruments *Instruments
}

func (*spanUsage) Name() string {
	return usageEnricherName
}

func (e *spanUsage) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	d := dialect.ForSpan(span)
	eventType := stated(d.EventType(span))
	if eventType != dialect.EventTypeAPIRequest {
		return nil, nil
	}

	var out []attribute.KeyValue
	if key, v, err := d.InputTokens(span); known(key, err) {
		out = append(out, AgentInputTokensKey.Int64(v))
	}
	if key, v, err := d.OutputTokens(span); known(key, err) {
		out = append(out, AgentOutputTokensKey.Int64(v))
	}
	if key, v, err := d.CacheReadTokens(span); known(key, err) {
		out = append(out, AgentCacheReadTokensKey.Int64(v))
	}
	if key, v, err := d.CacheWriteTokens(span); known(key, err) {
		out = append(out, AgentCacheWriteTokensKey.Int64(v))
	}
	if key, v, err := d.CostUSD(span); known(key, err) {
		out = append(out, AgentCostUSDKey.Float64(v))
	}

	countMissing(ctx, e.instruments, func() string { return missingLabel(d.Surface(span)) }, eventType, usageExpectations, out)
	return out, nil
}
