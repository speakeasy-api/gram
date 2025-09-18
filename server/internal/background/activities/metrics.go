package activities

import (
	"context"
	"log/slog"
	"time"

	"github.com/Masterminds/semver/v3"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

func newMeter(meterProvider metric.MeterProvider) metric.Meter {
	return meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities")
}

const (
	meterOpenAPIOperationsSkipped = "openapi.operations.skipped"
	meterOpenAPIUpgradeCounter    = "openapi.upgrade.count"
	meterOpenAPIUpgradeDuration   = "openapi.upgrade.duration"
	meterOpenAPIProcessedCounter  = "openapi.processed.count"
	meterOpenAPIProcessedDuration = "openapi.processed.duration"

	meterFunctionsToolsSkipped      = "functions.tools.skipped"
	meterFunctionsToolsCounter      = "functions.tools.count"
	meterFunctionsProcessedDuration = "functions.processed.duration"
)

type metrics struct {
	opSkipped metric.Int64Counter

	openAPIUpgradeCounter   metric.Int64Counter
	openAPIProcessedCounter metric.Int64Counter

	openAPIProcessedDuration metric.Float64Histogram
	openAPIUpgradeDuration   metric.Float64Histogram

	functionsToolsSkipped      metric.Int64Counter
	functionsToolsCounter      metric.Int64Counter
	functionsProcessedDuration metric.Float64Histogram
}

func newMetrics(meter metric.Meter, logger *slog.Logger) *metrics {
	ctx := context.Background()

	opSkipped, err := meter.Int64Counter(
		meterOpenAPIOperationsSkipped,
		metric.WithDescription("Number of OpenAPI operations that were skipped due to errors"),
		metric.WithUnit("{#}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric error", attr.SlogMetricName(meterOpenAPIOperationsSkipped), attr.SlogError(err))
	}

	openAPIUpgradeCounter, err := meter.Int64Counter(
		meterOpenAPIUpgradeCounter,
		metric.WithDescription("Number of OpenAPI 3.0 to 3.1 upgrades"),
		metric.WithUnit("{upgrade}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterOpenAPIUpgradeCounter), attr.SlogError(err))
	}

	openAPIProcessedCounter, err := meter.Int64Counter(
		meterOpenAPIProcessedCounter,
		metric.WithDescription("Number of processed openapi documents"),
		metric.WithUnit("{document}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterOpenAPIProcessedCounter), attr.SlogError(err))
	}

	openAPIProcessedDuration, err := meter.Float64Histogram(
		meterOpenAPIProcessedDuration,
		metric.WithDescription("Duration of openapi document processing in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 240),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterOpenAPIProcessedDuration), attr.SlogError(err))
	}

	openAPIUpgradeDuration, err := meter.Float64Histogram(
		meterOpenAPIUpgradeDuration,
		metric.WithDescription("Duration of openapi document upgrade in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(.05, .1, .25, .5, .75, 1, 2.5, 5, 7.5, 10, 25),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterOpenAPIUpgradeDuration), attr.SlogError(err))
	}

	functionsToolsSkipped, err := meter.Int64Counter(
		meterFunctionsToolsSkipped,
		metric.WithDescription("Number of functions tools that were skipped due to errors"),
		metric.WithUnit("{#}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterFunctionsToolsSkipped), attr.SlogError(err))
	}

	functionsToolsCounter, err := meter.Int64Counter(
		meterFunctionsToolsCounter,
		metric.WithDescription("Number of processed functions tools"),
		metric.WithUnit("{tool}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterFunctionsToolsCounter), attr.SlogError(err))
	}

	functionsProcessedDuration, err := meter.Float64Histogram(
		meterFunctionsProcessedDuration,
		metric.WithDescription("Duration of functions processing in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 240),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterFunctionsProcessedDuration), attr.SlogError(err))
	}

	return &metrics{
		opSkipped:                  opSkipped,
		openAPIUpgradeCounter:      openAPIUpgradeCounter,
		openAPIProcessedCounter:    openAPIProcessedCounter,
		openAPIProcessedDuration:   openAPIProcessedDuration,
		openAPIUpgradeDuration:     openAPIUpgradeDuration,
		functionsToolsSkipped:      functionsToolsSkipped,
		functionsToolsCounter:      functionsToolsCounter,
		functionsProcessedDuration: functionsProcessedDuration,
	}
}

func (m *metrics) RecordOpenAPIOperationSkipped(ctx context.Context, reason string) {
	if counter := m.opSkipped; counter != nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attr.Reason(reason),
		))
	}
}

func (m *metrics) RecordOpenAPIProcessed(ctx context.Context, parser string, outcome o11y.Outcome, duration time.Duration, version string) {
	if counter := m.openAPIProcessedCounter; counter != nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attr.Outcome(outcome),
			attr.OpenAPIVersion(sanitizeOpenAPIVersion(version)),
			attr.DeploymentOpenAPIParser(parser),
		))
	}

	if histogram := m.openAPIProcessedDuration; histogram != nil {
		histogram.Record(ctx, duration.Seconds(), metric.WithAttributes(
			attr.Outcome(outcome),
			attr.DeploymentOpenAPIParser(parser),
		))
	}
}

func (m *metrics) RecordOpenAPIUpgrade(ctx context.Context, parser string, outcome o11y.Outcome, duration time.Duration, version string) {
	if counter := m.openAPIUpgradeCounter; counter != nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attr.Outcome(string(outcome)),
			attr.OpenAPIVersion(sanitizeOpenAPIVersion(version)),
			attr.DeploymentOpenAPIParser(parser),
		))
	}

	if histogram := m.openAPIUpgradeDuration; histogram != nil {
		histogram.Record(ctx, duration.Seconds(), metric.WithAttributes(
			attr.Outcome(outcome),
			attr.DeploymentOpenAPIParser(parser),
		))
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

func (m *metrics) RecordFunctionsToolSkipped(ctx context.Context, reason string) {
	if counter := m.functionsToolsSkipped; counter != nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attr.Reason(reason),
		))
	}
}

func (m *metrics) RecordFunctionsProcessed(
	ctx context.Context,
	duration time.Duration,
	outcome o11y.Outcome,
	manifestVersion string,
	numTools int,
	toolRuntime string,
) {
	if counter := m.functionsToolsCounter; counter != nil {
		counter.Add(ctx, int64(numTools), metric.WithAttributes(
			attr.FunctionsToolRuntime(toolRuntime),
		))
	}

	if histogram := m.functionsProcessedDuration; histogram != nil {
		histogram.Record(ctx, duration.Seconds(), metric.WithAttributes(
			attr.Outcome(outcome),
			attr.FunctionsManifestVersion(manifestVersion),
			attr.FunctionsToolRuntime(toolRuntime),
		))
	}
}
