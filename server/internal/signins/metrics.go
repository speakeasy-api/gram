package signins

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/signins/repo"
)

const (
	meterRecorded = "sign_in.recorded"
	meterRevoked  = "sign_in.revoked"
	// meterListingFailed counts sign outs everywhere that ended only the
	// WorkOS sessions already known.
	meterListingFailed = "sign_in.session_listing_failed"
)

// Record outcomes.
const (
	OutcomeRecorded             = "recorded"
	OutcomeRecordedRevoked      = "recorded_revoked"
	OutcomeNoSession            = "no_session"
	OutcomeNoOrganization       = "no_organization"
	OutcomeUnknownOrganization  = "unknown_organization"
	OutcomeOrganizationMismatch = "organization_mismatch"
	OutcomeImpersonated         = "impersonated"
	OutcomeOtherUser            = "other_user"
	OutcomeError                = "error"
)

type instruments struct {
	recorded      metric.Int64Counter
	revoked       metric.Int64Counter
	listingFailed metric.Int64Counter
}

var (
	instrumentsOnce sync.Once
	meters          instruments
)

func loadInstruments() instruments {
	instrumentsOnce.Do(func() {
		meter := otel.GetMeterProvider().Meter("github.com/speakeasy-api/gram/server/internal/signins")
		if c, err := meter.Int64Counter(meterRecorded, metric.WithDescription("WorkOS code exchanges by flow and how they resolved to a sign-in."), metric.WithUnit("{exchange}")); err == nil {
			meters.recorded = c
		}
		if c, err := meter.Int64Counter(meterListingFailed, metric.WithDescription("Sign outs everywhere that could not list the user's WorkOS sessions."), metric.WithUnit("{sign_out}")); err == nil {
			meters.listingFailed = c
		}
		if c, err := meter.Int64Counter(meterRevoked, metric.WithDescription("Sign-ins revoked, by reason."), metric.WithUnit("{sign_in}")); err == nil {
			meters.revoked = c
		}
	})
	return meters
}

func recordOutcome(ctx context.Context, flow, outcome string) {
	if c := loadInstruments().recorded; c != nil {
		c.Add(ctx, 1, metric.WithAttributes(attr.AuthSignInFlow(flow), attr.SignInRecordOutcome(outcome)))
	}
}

// RecordSessionListingFailed counts sign outs everywhere that could not list
// the user's WorkOS sessions and ended only the known ones.
func RecordSessionListingFailed(ctx context.Context) {
	if c := loadInstruments().listingFailed; c != nil {
		c.Add(ctx, 1)
	}
}

// RecordRevoked counts newly revoked sign-ins.
func RecordRevoked(ctx context.Context, reason RevokeReason, n int) {
	if n <= 0 {
		return
	}
	if c := loadInstruments().revoked; c != nil {
		c.Add(ctx, int64(n), metric.WithAttributes(attr.SignInRevokeReason(string(reason))))
	}
}

const (
	meterDeliveryAttempts = "sign_in.revocation_delivery.attempts"
	meterDeliveryPending  = "sign_in.revocation_delivery.pending"
	meterDeliveryStuck    = "sign_in.revocation_delivery.stuck"
	meterDeliveryOldest   = "sign_in.revocation_delivery.oldest_age"
)

type deliveryMetrics struct {
	attempts metric.Int64Counter
	pending  metric.Int64Gauge
	stuck    metric.Int64Gauge
	oldest   metric.Int64Gauge
}

func newDeliveryMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) deliveryMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/signins")
	var m deliveryMetrics
	var err error
	if m.attempts, err = meter.Int64Counter(meterDeliveryAttempts, metric.WithDescription("Sign-in revocation delivery attempts by kind and outcome."), metric.WithUnit("{attempt}")); err != nil {
		logger.ErrorContext(ctx, "create revocation delivery attempts counter", attr.SlogError(err))
	}
	if m.pending, err = meter.Int64Gauge(meterDeliveryPending, metric.WithDescription("Undelivered sign-in revocation deliveries."), metric.WithUnit("{delivery}")); err != nil {
		logger.ErrorContext(ctx, "create revocation delivery pending gauge", attr.SlogError(err))
	}
	if m.stuck, err = meter.Int64Gauge(meterDeliveryStuck, metric.WithDescription("Undelivered sign-in revocation deliveries that failed repeatedly."), metric.WithUnit("{delivery}")); err != nil {
		logger.ErrorContext(ctx, "create revocation delivery stuck gauge", attr.SlogError(err))
	}
	if m.oldest, err = meter.Int64Gauge(meterDeliveryOldest, metric.WithDescription("Age of the oldest undelivered sign-in revocation delivery."), metric.WithUnit("s")); err != nil {
		logger.ErrorContext(ctx, "create revocation delivery age gauge", attr.SlogError(err))
	}
	return m
}

func (m deliveryMetrics) recordAttempt(ctx context.Context, kind, outcome string) {
	if m.attempts == nil {
		return
	}
	m.attempts.Add(ctx, 1, metric.WithAttributes(attr.SignInDeliveryKind(kind), attr.Outcome(outcome)))
}

func (m deliveryMetrics) recordBacklog(ctx context.Context, b repo.RevocationDeliveryBacklogRow) {
	if m.pending != nil {
		m.pending.Record(ctx, b.Pending)
	}
	if m.stuck != nil {
		m.stuck.Record(ctx, b.Stuck)
	}
	if m.oldest != nil {
		m.oldest.Record(ctx, b.OldestSeconds)
	}
}
