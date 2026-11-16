package orgencryption

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// meterTransitionRuns counts runner runs by use case and outcome. A run
	// whose verification finds copies that differ or do not open is a
	// failure even though it returns no error.
	meterTransitionRuns = "gram.orgencryption.transition.runs"

	// meterTransitionConverted counts values that gained an organization-key
	// copy through the backfill, by use case.
	meterTransitionConverted = "gram.orgencryption.transition.converted"

	// meterTransitionWaiting is the number of values still waiting for an
	// organization-key copy after an organization's last run, by use case
	// and organization.
	meterTransitionWaiting = "gram.orgencryption.transition.waiting"

	// meterTransitionUnreadable is the number of values recorded as
	// unreadable after an organization's last run, by use case and
	// organization.
	meterTransitionUnreadable = "gram.orgencryption.transition.unreadable"

	// meterTransitionUnattributed is the number of values stored without an
	// organization they can be attributed to after an organization's last
	// run, by use case and organization. Finalize refuses while it is above
	// zero.
	meterTransitionUnattributed = "gram.orgencryption.transition.unattributed"

	// meterTransitionVerificationFailures counts values whose copies did not
	// match or whose organization-key copy did not open during
	// verification, by use case. Any increase blocks finalize and needs a
	// person to look.
	meterTransitionVerificationFailures = "gram.orgencryption.transition.verification_failures"
)

// runnerMetrics holds the rollout instruments. Instruments are nil-guarded,
// so a construction failure degrades to no metrics rather than a panic.
type runnerMetrics struct {
	runs                 metric.Int64Counter
	converted            metric.Int64Counter
	waiting              metric.Int64Gauge
	unreadable           metric.Int64Gauge
	unattributed         metric.Int64Gauge
	verificationFailures metric.Int64Counter
}

func newRunnerMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *runnerMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/orgencryption")

	logErr := func(name string, err error) {
		if err != nil {
			logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(name), attr.SlogError(err))
		}
	}

	runs, err := meter.Int64Counter(meterTransitionRuns,
		metric.WithDescription("Organization encryption transition runs by use case and outcome."),
		metric.WithUnit("{run}"))
	logErr(meterTransitionRuns, err)

	converted, err := meter.Int64Counter(meterTransitionConverted,
		metric.WithDescription("Values that gained an organization-key copy through the backfill."),
		metric.WithUnit("{value}"))
	logErr(meterTransitionConverted, err)

	waiting, err := meter.Int64Gauge(meterTransitionWaiting,
		metric.WithDescription("Values still waiting for an organization-key copy after an organization's last run."),
		metric.WithUnit("{value}"))
	logErr(meterTransitionWaiting, err)

	unreadable, err := meter.Int64Gauge(meterTransitionUnreadable,
		metric.WithDescription("Values recorded as unreadable after an organization's last run."),
		metric.WithUnit("{value}"))
	logErr(meterTransitionUnreadable, err)

	unattributed, err := meter.Int64Gauge(meterTransitionUnattributed,
		metric.WithDescription("Values stored without an organization they can be attributed to after an organization's last run."),
		metric.WithUnit("{value}"))
	logErr(meterTransitionUnattributed, err)

	verificationFailures, err := meter.Int64Counter(meterTransitionVerificationFailures,
		metric.WithDescription("Values whose copies did not match, or whose organization-key copy did not open, during verification."),
		metric.WithUnit("{value}"))
	logErr(meterTransitionVerificationFailures, err)

	return &runnerMetrics{runs: runs, converted: converted, waiting: waiting, unreadable: unreadable, unattributed: unattributed, verificationFailures: verificationFailures}
}

func (m *runnerMetrics) record(ctx context.Context, organizationID string, useCase UseCase, result RunResult, err error) {
	byUseCase := metric.WithAttributes(attr.OrgEncryptionUseCase(useCase))
	byOrganization := metric.WithAttributes(attr.OrgEncryptionUseCase(useCase), attr.OrganizationID(organizationID))

	if m.runs != nil {
		outcome := o11y.OutcomeFromError(err)
		if err == nil && result.Mismatched+result.Unopenable > 0 {
			outcome = o11y.OutcomeFailure
		}
		m.runs.Add(ctx, 1, metric.WithAttributes(attr.OrgEncryptionUseCase(useCase), attr.Outcome(outcome)))
	}
	if m.converted != nil && result.Converted > 0 {
		m.converted.Add(ctx, result.Converted, byUseCase)
	}
	if m.verificationFailures != nil && result.Mismatched+result.Unopenable > 0 {
		m.verificationFailures.Add(ctx, result.Mismatched+result.Unopenable, byUseCase)
	}
	if !result.Counted {
		return
	}
	if m.waiting != nil {
		m.waiting.Record(ctx, result.Remaining, byOrganization)
	}
	if m.unreadable != nil {
		m.unreadable.Record(ctx, result.Unreadable, byOrganization)
	}
	if m.unattributed != nil {
		m.unattributed.Record(ctx, result.Unattributed, byOrganization)
	}
}
