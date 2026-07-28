package hooks

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

const (
	meterHooksEventDuration = "hooks.event.duration"

	hookMetricOutcomeAccepted        = "accepted"
	hookMetricOutcomeFailure         = "failure"
	hookMetricOutcomeUnauthorized    = "unauthorized"
	hookMetricOutcomeUnauthenticated = "unauthenticated"

	hookMetricDecisionAllow = "allow"
	hookMetricDecisionDeny  = "deny"
	// hookMetricDecisionNone marks responses that carried no verdict at all —
	// the endpoint errored before producing a result. Client behavior then
	// depends on the org's fail open/closed setting, so neither allow nor deny
	// would be truthful.
	hookMetricDecisionNone = "none"
)

type metrics struct {
	eventDuration metric.Float64Histogram
}

func newMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *metrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/hooks")

	eventDuration, err := meter.Float64Histogram(
		meterHooksEventDuration,
		metric.WithDescription("Duration of hook endpoint event processing in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create metric", attr.SlogMetricName(meterHooksEventDuration), attr.SlogError(err))
	}

	return &metrics{
		eventDuration: eventDuration,
	}
}

func (m *metrics) RecordHookEventDuration(ctx context.Context, source string, eventName string, outcome string, decision string, orgSlug string, riskScanned bool, duration time.Duration) {
	if m == nil || m.eventDuration == nil {
		return
	}

	m.eventDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(hookEventMetricAttributes(source, eventName, outcome, decision, orgSlug, riskScanned)...))
}

// claudeHookDecision maps a Claude hook response to the verdict dimension on
// hook metrics: a top-level block or a nested permissionDecision of deny/ask
// wins; anything else — including responses with no decision field, which
// Claude Code treats as pass-through — counts as allow.
func claudeHookDecision(res *gen.ClaudeHookResult) string {
	if res == nil {
		return hookMetricDecisionNone
	}
	if conv.PtrValOr(res.Decision, "") == "block" {
		return hookMetricDecisionDeny
	}
	if output, ok := res.HookSpecificOutput.(*HookSpecificOutput); ok && output != nil {
		if decision := conv.PtrValOr(output.PermissionDecision, ""); decision != "" {
			return decision
		}
	}
	return hookMetricDecisionAllow
}

// cursorHookDecision and codexHookDecision pass the response verdict through
// (allow / deny / ask for Cursor), defaulting absent fields to allow to match
// how the client interprets them.
func cursorHookDecision(res *gen.CursorHookResult) string {
	if res == nil {
		return hookMetricDecisionNone
	}
	return conv.Default(conv.PtrValOr(res.Permission, ""), hookMetricDecisionAllow)
}

func codexHookDecision(res *gen.CodexHookResult) string {
	if res == nil {
		return hookMetricDecisionNone
	}
	return conv.Default(conv.PtrValOr(res.Decision, ""), hookMetricDecisionAllow)
}

func hookEventMetricAttributes(source string, eventName string, outcome string, decision string, orgSlug string, riskScanned bool) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attr.HookSource(source),
		attr.HookEvent(eventName),
		attr.Outcome(outcome),
		attr.HookDecision(decision),
		attr.HookRiskScanned(riskScanned),
	}
	if orgSlug != "" {
		attrs = append(attrs, attr.OrganizationSlug(orgSlug))
	}
	return attrs
}
