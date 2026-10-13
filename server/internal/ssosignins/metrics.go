package ssosignins

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterRecorded = "sso_sign_in.recorded"
	meterRevoked  = "sso_sign_in.revoked"
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
	recorded metric.Int64Counter
	revoked  metric.Int64Counter
}

var (
	instrumentsOnce sync.Once
	meters          instruments
)

func loadInstruments() instruments {
	instrumentsOnce.Do(func() {
		meter := otel.GetMeterProvider().Meter("github.com/speakeasy-api/gram/server/internal/ssosignins")
		if c, err := meter.Int64Counter(meterRecorded, metric.WithDescription("WorkOS code exchanges by flow and how they resolved to an SSO sign-in."), metric.WithUnit("{exchange}")); err == nil {
			meters.recorded = c
		}
		if c, err := meter.Int64Counter(meterRevoked, metric.WithDescription("SSO sign-ins revoked, by reason."), metric.WithUnit("{sign_in}")); err == nil {
			meters.revoked = c
		}
	})
	return meters
}

func recordOutcome(ctx context.Context, flow, outcome string) {
	if c := loadInstruments().recorded; c != nil {
		c.Add(ctx, 1, metric.WithAttributes(attr.AuthSignInFlow(flow), attr.SSOSignInRecordOutcome(outcome)))
	}
}

// RecordRevoked counts newly revoked sign-ins.
func RecordRevoked(ctx context.Context, reason RevokeReason, n int) {
	if n <= 0 {
		return
	}
	if c := loadInstruments().revoked; c != nil {
		c.Add(ctx, int64(n), metric.WithAttributes(attr.SSOSignInRevokeReason(string(reason))))
	}
}
