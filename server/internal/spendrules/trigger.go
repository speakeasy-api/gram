package spendrules

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/throttle"
)

// usageSignalTimeout bounds one signal attempt (a Redis rules read plus a
// Temporal SignalWithStart). It caps how long a fired signal can run so a slow
// dependency cannot pin a leading-edge goroutine or hold graceful shutdown open
// while trailing signals flush.
const usageSignalTimeout = 10 * time.Second

// UsageSignalCooldown is the per-actor throttle window for
// usage-triggered evaluation signals. The first spend-relevant telemetry
// write for an actor signals immediately (leading edge); writes inside the
// window coalesce into one trailing signal, so breach-to-block latency is
// bounded by roughly this window plus one evaluation instead of the
// scheduled sweep interval.
const UsageSignalCooldown = 30 * time.Second

// isSpendRelevantURN mirrors the row predicates of
// spend_rule_usage_summaries_mv (server/clickhouse/schema.sql): only Claude
// Code OTEL logs and Codex/Cursor usage rows feed spend-rule enforcement.
// The check is deliberately looser than the MV (no api_request filtering) —
// a spurious trigger costs one throttled no-op evaluation.
func isSpendRelevantURN(urn string) bool {
	return urn == "claude-code:otel:logs" ||
		strings.HasPrefix(urn, "codex:usage") ||
		strings.HasPrefix(urn, "cursor:usage")
}

// UsageTrigger turns spend-relevant telemetry writes into debounced per-actor
// evaluation signals. It observes the telemetry logger, throttles per Gram user
// ID, and only signals organizations that currently have spend gate rules.
type UsageTrigger struct {
	logger   *slog.Logger
	cache    cache.Cache
	signaler ActorEvaluationSignaler
	throttle *throttle.Throttle[string, ActorEvaluationSignal]
}

var _ telemetry.LogObserver = (*UsageTrigger)(nil)

func NewUsageTrigger(logger *slog.Logger, cacheImpl cache.Cache, signaler ActorEvaluationSignaler, cooldown time.Duration) *UsageTrigger {
	t := &UsageTrigger{
		logger:   logger.With(attr.SlogComponent("spendrules_usage_trigger")),
		cache:    cacheImpl,
		signaler: signaler,
		throttle: nil,
	}
	t.throttle = throttle.New(cooldown, usageSignalKey, func(signal ActorEvaluationSignal) error {
		// Trailing edge fires from a timer goroutine after the request
		// context that suppressed it is gone. Bound it so a stuck Redis or
		// Temporal call cannot outlive the graceful-shutdown flush.
		bctx, cancel := context.WithTimeout(context.Background(), usageSignalTimeout)
		defer cancel()
		t.signal(bctx, signal)
		return nil
	})
	return t
}

func (t *UsageTrigger) OnTelemetryLogsWritten(ctx context.Context, params []telemetry.LogParams) {
	seen := make(map[string]struct{}, 1)
	for _, p := range params {
		organizationID := p.ToolInfo.OrganizationID
		if organizationID == "" || !isSpendRelevantURN(p.ToolInfo.URN) {
			continue
		}
		signal := ActorEvaluationSignal{
			OrganizationID: organizationID,
			UserID:         p.UserInfo.UserID(),
			Email:          p.UserInfo.Email(),
		}
		if signal.UserID == "" {
			continue
		}
		key := usageSignalKey(signal)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if t.throttle.Do(signal) {
			t.signalAsync(ctx, signal)
		}
	}
}

func usageSignalKey(signal ActorEvaluationSignal) string {
	return signal.OrganizationID + ":" + signal.UserID
}

// signalAsync fires a leading-edge signal off the telemetry write path. The
// observer runs inside LogBulk (and inside poll activities whose context is
// canceled the instant they return), so the signal must not block the caller
// and must survive that cancellation: it detaches the context, keeps only its
// values, and bounds the work with usageSignalTimeout. A completed ClickHouse
// write therefore still wakes enforcement even when the request/activity that
// produced it is already gone.
func (t *UsageTrigger) signalAsync(ctx context.Context, signal ActorEvaluationSignal) {
	detached := context.WithoutCancel(ctx)
	go func() {
		bctx, cancel := context.WithTimeout(detached, usageSignalTimeout)
		defer cancel()
		t.signal(bctx, signal)
	}()
}

func (t *UsageTrigger) signal(ctx context.Context, signal ActorEvaluationSignal) {
	var rules GateRules
	err := t.cache.Get(ctx, spendGateRulesKey(signal.OrganizationID), &rules)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return
	}
	if err != nil {
		t.logger.WarnContext(ctx, "read spend gate rules for usage trigger",
			attr.SlogError(err),
			attr.SlogOrganizationID(signal.OrganizationID),
		)
		return
	}
	if len(rules.Rules) == 0 {
		return
	}

	if err := t.signaler.SignalActor(ctx, signal); err != nil {
		t.logger.WarnContext(ctx, "signal spend rule evaluation for fresh usage",
			attr.SlogError(err),
			attr.SlogOrganizationID(signal.OrganizationID),
		)
	}
}

// Shutdown flushes pending trailing signals. Call it while the Temporal
// client is still open — after traffic has drained, before runShutdown
// closes the client.
func (t *UsageTrigger) Shutdown(context.Context) error {
	t.throttle.Flush()
	return nil
}
