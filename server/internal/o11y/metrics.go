package o11y

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ⚠️ Use custom metrics judiciously, excessive tag cardinality for custom metrics (user IDs, request IDs) can become expensive
// See: https://docs.datadoghq.com/account_management/billing/custom_metrics/?tab=countrate
// Metrics should be used for high value telemetry on key events

const (
	meterToolCallCounter          = "tool.call"
	meterOpenAPIUpgradeCounter    = "openapi.upgrade.count"
	meterOpenAPIUpgradeDuration   = "openapi.upgrade.duration"
	meterOpenAPIProcessedCounter  = "openapi.processed.count"
	meterOpenAPIProcessedDuration = "openapi.processed.duration"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

func OutcomeFromError(err error) Outcome {
	if err == nil {
		return OutcomeSuccess
	}
	return OutcomeFailure
}

type Metrics struct {
	meter      metric.Meter
	counters   map[string]metric.Int64Counter
	histograms map[string]metric.Float64Histogram
}

func NewMetrics(provider metric.MeterProvider) (*Metrics, error) {
	var err error
	var counters = make(map[string]metric.Int64Counter)
	var histograms = make(map[string]metric.Float64Histogram)

	meter := provider.Meter("gram")

	if counters[meterToolCallCounter], err = meter.Int64Counter(
		meterToolCallCounter,
		metric.WithDescription("Number of HTTP tool calls"),
		metric.WithUnit("{call}"),
	); err != nil {
		return nil, fmt.Errorf("create counter %s: %w", meterToolCallCounter, err)
	}

	if counters[meterOpenAPIUpgradeCounter], err = meter.Int64Counter(
		meterOpenAPIUpgradeCounter,
		metric.WithDescription("Number of OpenAPI 3.0 to 3.1 upgrades"),
		metric.WithUnit("{upgrade}"),
	); err != nil {
		return nil, fmt.Errorf("create counter %s: %w", meterOpenAPIUpgradeCounter, err)
	}

	if counters[meterOpenAPIProcessedCounter], err = meter.Int64Counter(
		meterOpenAPIProcessedCounter,
		metric.WithDescription("Number of processed openapi documents"),
		metric.WithUnit("{document}"),
	); err != nil {
		return nil, fmt.Errorf("create counter %s: %w", meterOpenAPIProcessedCounter, err)
	}

	if histograms[meterOpenAPIProcessedDuration], err = meter.Float64Histogram(
		meterOpenAPIProcessedDuration,
		metric.WithDescription("Duration of openapi document processing in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 240),
	); err != nil {
		return nil, fmt.Errorf("create histogram %s: %w", meterOpenAPIProcessedDuration, err)
	}

	if histograms[meterOpenAPIUpgradeDuration], err = meter.Float64Histogram(
		meterOpenAPIUpgradeDuration,
		metric.WithDescription("Duration of openapi document upgrade in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(.05, .1, .25, .5, .75, 1, 2.5, 5, 7.5, 10, 25),
	); err != nil {
		return nil, fmt.Errorf("create histogram %s: %w", meterOpenAPIUpgradeDuration, err)
	}

	return &Metrics{
		meter:      meter,
		counters:   counters,
		histograms: histograms,
	}, nil
}

func (m *Metrics) RecordHTTPToolCall(ctx context.Context, projectID uuid.UUID, toolName string, statusCode int) {
	if counter, ok := m.counters[meterToolCallCounter]; ok {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("tool", toolName),
			attribute.String("project_id", projectID.String()),
			attribute.String("status_code", fmt.Sprintf("%d", statusCode)),
		))
	}
}

func (m *Metrics) RecordOpenAPIProcessed(ctx context.Context, outcome Outcome, duration time.Duration, version string) {
	if counter, ok := m.counters[meterOpenAPIProcessedCounter]; ok {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("outcome", string(outcome)),
			attribute.String("openapi.version", sanitizeOpenAPIVersion(version)),
		))
	}

	if histogram, ok := m.histograms[meterOpenAPIProcessedDuration]; ok {
		histogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attribute.String("outcome", string(outcome))))
	}
}

func (m *Metrics) RecordOpenAPIUpgrade(ctx context.Context, outcome Outcome, duration time.Duration, version string) {
	if counter, ok := m.counters[meterOpenAPIUpgradeCounter]; ok {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("outcome", string(outcome)),
			attribute.String("openapi.version", sanitizeOpenAPIVersion(version)),
		))
	}

	if histogram, ok := m.histograms[meterOpenAPIUpgradeDuration]; ok {
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
