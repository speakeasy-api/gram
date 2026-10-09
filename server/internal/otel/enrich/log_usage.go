package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// logUsage writes what an api_request used. Only a request carries usage; a
// compaction's before and after counts are housekeeping and stay in its
// payload. A stated zero is written.
type logUsage struct{}

func (*logUsage) Name() string {
	return usageEnricherName
}

func (*logUsage) Enrich(_ context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	eventType := stated(d.EventType(record))
	if eventType != dialect.EventTypeAPIRequest {
		return nil, nil
	}

	var out []attribute.KeyValue
	if key, v, err := d.InputTokens(record); known(key, err) {
		out = append(out, AgentInputTokensKey.Int64(v))
	}
	if key, v, err := d.OutputTokens(record); known(key, err) {
		out = append(out, AgentOutputTokensKey.Int64(v))
	}
	if key, v, err := d.CacheReadTokens(record); known(key, err) {
		out = append(out, AgentCacheReadTokensKey.Int64(v))
	}
	if key, v, err := d.CacheWriteTokens(record); known(key, err) {
		out = append(out, AgentCacheWriteTokensKey.Int64(v))
	}
	if key, v, err := d.CostUSD(record); known(key, err) {
		out = append(out, AgentCostUSDKey.Float64(v))
	}
	return out, nil
}
