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
type logUsage struct {
	instruments *Instruments
}

func (*logUsage) Name() string {
	return usageEnricherName
}

func (e *logUsage) Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	eventType := stated(d.EventType(record))
	if eventType != dialect.EventTypeAPIRequest {
		return nil, nil
	}

	var out []attribute.KeyValue
	if key, v, err := d.InputTokens(record); known(key, err) {
		out = append(out, InputTokensColumnKey.Int64(v))
	}
	if key, v, err := d.OutputTokens(record); known(key, err) {
		out = append(out, OutputTokensColumnKey.Int64(v))
	}
	if key, v, err := d.CacheReadTokens(record); known(key, err) {
		out = append(out, CacheReadTokensColumnKey.Int64(v))
	}
	if key, v, err := d.CacheWriteTokens(record); known(key, err) {
		out = append(out, CacheWriteTokensColumnKey.Int64(v))
	}
	if key, v, err := d.CostUSD(record); known(key, err) {
		out = append(out, CostUSDColumnKey.Float64(v))
	}

	countMissing(ctx, e.instruments, func() string { return missingLabel(d.Surface(record)) }, eventType, usageExpectations, out)
	return out, nil
}
