package activities

import (
	"context"
	"log/slog"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func newMeter(meterProvider metric.MeterProvider) metric.Meter {
	return meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities")
}

const (
	metricOpenAPIOperationsSkipped = "openapi.operations.skipped"
	meterOpenAPIUpgradeCounter     = "openapi.upgrade.count"
	meterOpenAPIUpgradeDuration    = "openapi.upgrade.duration"
	meterOpenAPIProcessedCounter   = "openapi.processed.count"
	meterOpenAPIProcessedDuration  = "openapi.processed.duration"
)

type metrics struct {
	opSkipped metric.Int64Counter

	openAPIUpgradeCounter   metric.Int64Counter
	openAPIProcessedCounter metric.Int64Counter

	openAPIProcessedDuration metric.Float64Histogram
	openAPIUpgradeDuration   metric.Float64Histogram
}

func newMetrics(meter metric.Meter, logger *slog.Logger) *metrics {
	ctx := context.Background()

	opSkipped, err := meter.Int64Counter(
		metricOpenAPIOperationsSkipped,
		metric.WithDescription("Number of OpenAPI operations that were skipped due to errors"),
		metric.WithUnit("{#}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric error", slog.String("metric", metricOpenAPIOperationsSkipped), slog.String("error", err.Error()))
	}

	openAPIUpgradeCounter, err := meter.Int64Counter(
		meterOpenAPIUpgradeCounter,
		metric.WithDescription("Number of OpenAPI 3.0 to 3.1 upgrades"),
		metric.WithUnit("{upgrade}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", slog.String("name", meterOpenAPIUpgradeCounter), slog.String("error", err.Error()))
	}

	openAPIProcessedCounter, err := meter.Int64Counter(
		meterOpenAPIProcessedCounter,
		metric.WithDescription("Number of processed openapi documents"),
		metric.WithUnit("{document}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", slog.String("name", meterOpenAPIProcessedCounter), slog.String("error", err.Error()))
	}

	openAPIProcessedDuration, err := meter.Float64Histogram(
		meterOpenAPIProcessedDuration,
		metric.WithDescription("Duration of openapi document processing in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 240),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", slog.String("name", meterOpenAPIProcessedDuration), slog.String("error", err.Error()))
	}

	openAPIUpgradeDuration, err := meter.Float64Histogram(
		meterOpenAPIUpgradeDuration,
		metric.WithDescription("Duration of openapi document upgrade in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(.05, .1, .25, .5, .75, 1, 2.5, 5, 7.5, 10, 25),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", slog.String("name", meterOpenAPIUpgradeDuration), slog.String("error", err.Error()))
	}

	return &metrics{
		opSkipped:                opSkipped,
		openAPIUpgradeCounter:    openAPIUpgradeCounter,
		openAPIProcessedCounter:  openAPIProcessedCounter,
		openAPIProcessedDuration: openAPIProcessedDuration,
		openAPIUpgradeDuration:   openAPIUpgradeDuration,
	}
}

func (m *metrics) RecordOpenAPIOperationSkipped(ctx context.Context, reason string) {
	if counter := m.opSkipped; counter != nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", reason),
		))
	}
}

func (m *metrics) RecordOpenAPIProcessed(ctx context.Context, outcome o11y.Outcome, duration time.Duration, version string) {
	if counter := m.openAPIProcessedCounter; counter != nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("outcome", string(outcome)),
			attribute.String("openapi.version", sanitizeOpenAPIVersion(version)),
		))
	}

	if histogram := m.openAPIProcessedDuration; histogram != nil {
		histogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attribute.String("outcome", string(outcome))))
	}
}

func (m *metrics) RecordOpenAPIUpgrade(ctx context.Context, outcome o11y.Outcome, duration time.Duration, version string) {
	if counter := m.openAPIUpgradeCounter; counter != nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("outcome", string(outcome)),
			attribute.String("openapi.version", sanitizeOpenAPIVersion(version)),
		))
	}

	if histogram := m.openAPIUpgradeDuration; histogram != nil {
		histogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attribute.String("outcome", string(outcome))))
	}
}

func sanitizeOpenAPIVersion(version string) string {
	v := ""
	sv, err := semver.NewVersion(version)
	if err == nil && sv.Major() == 3 && sv.Prerelease() == "" && sv.Metadata() == "" {
		v = sv.String()
	}

	return v
}
