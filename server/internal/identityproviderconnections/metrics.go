package identityproviderconnections

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterVerifyOutcome = "gram.identity_provider_connection.verify.outcome"
	meterCreateOutcome = "gram.identity_provider_connection.create.outcome"
)

// Verify failure outcomes; a completed run records its status (verified or degraded).
const (
	verifyOutcomeCredentialRejected = "credential_rejected" //nolint:gosec // G101 false positive: a metric attribute value.
	verifyOutcomeUnreachable        = "unreachable"
)

// Create outcomes.
const (
	createOutcomeCreated         = "created"
	createOutcomeConflict        = "conflict"
	createOutcomeDiscoveryFailed = "discovery_failed"
	createOutcomeProvisionFailed = "provision_failed"
)

// serviceMetrics instruments are nil-guarded at every call site.
type serviceMetrics struct {
	verify metric.Int64Counter
	create metric.Int64Counter
}

func newServiceMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *serviceMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/identityproviderconnections")

	verify, err := meter.Int64Counter(
		meterVerifyOutcome,
		metric.WithDescription("Identity provider connection verifications by provider and outcome."),
		metric.WithUnit("{run}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterVerifyOutcome), attr.SlogError(err))
	}

	create, err := meter.Int64Counter(
		meterCreateOutcome,
		metric.WithDescription("Identity provider connection create attempts by provider and outcome."),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterCreateOutcome), attr.SlogError(err))
	}

	return &serviceMetrics{verify: verify, create: create}
}

func (m *serviceMetrics) recordVerify(ctx context.Context, provider, outcome string) {
	if m.verify == nil {
		return
	}
	m.verify.Add(ctx, 1, metric.WithAttributes(attr.Provider(provider), attr.Outcome(outcome)))
}

func (m *serviceMetrics) recordCreate(ctx context.Context, provider, outcome string) {
	if m.create == nil {
		return
	}
	m.create.Add(ctx, 1, metric.WithAttributes(attr.Provider(provider), attr.Outcome(outcome)))
}
