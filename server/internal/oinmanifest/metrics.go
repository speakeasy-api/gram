package oinmanifest

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// meterExport counts manifest exports by format. Together with the audit log
// line it makes every export visible in Datadog without a platform-scoped
// audit table.
const meterExport = "gram.oin_manifest.export"

type exportMetrics struct {
	exports metric.Int64Counter
}

func newExportMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *exportMetrics {
	if meterProvider == nil {
		return &exportMetrics{exports: nil}
	}
	counter, err := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/oinmanifest").Int64Counter(
		meterExport,
		metric.WithDescription("OIN Cross App Access manifest exports by format."),
		metric.WithUnit("{export}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterExport), attr.SlogError(err))
		return &exportMetrics{exports: nil}
	}
	return &exportMetrics{exports: counter}
}

func (m *exportMetrics) recordExport(ctx context.Context, format string) {
	if m == nil || m.exports == nil {
		return
	}
	m.exports.Add(ctx, 1, metric.WithAttributes(attr.OINManifestFormat(format)))
}
