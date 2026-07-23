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

// UsageSignalCooldown is the per-organization throttle window for
// usage-triggered evaluation signals. The first spend-relevant telemetry
// write for an org signals immediately (leading edge); writes inside the
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

// UsageTrigger turns spend-relevant telemetry writes into debounced per-org
// evaluation signals. It observes the telemetry logger, throttles per
// organization, and only signals organizations that currently have a spend
// gate snapshot — the evaluator maintains one for every org with enabled
// rules and the budgets flag on, so its absence means evaluation would be a
// no-op. Orgs whose snapshot is missing (first rule just created, Redis
// flush) are covered by the mutation-time signal and the scheduled sweep.
type UsageTrigger struct {
	logger   *slog.Logger
	cache    cache.Cache
	signaler EvaluationSignaler
	throttle *throttle.Throttle[string, string]
}

var _ telemetry.LogObserver = (*UsageTrigger)(nil)

func NewUsageTrigger(logger *slog.Logger, cacheImpl cache.Cache, signaler EvaluationSignaler, cooldown time.Duration) *UsageTrigger {
	t := &UsageTrigger{
		logger:   logger.With(attr.SlogComponent("spendrules_usage_trigger")),
		cache:    cacheImpl,
		signaler: signaler,
		throttle: nil,
	}
	t.throttle = throttle.New(cooldown, func(organizationID string) string {
		return organizationID
	}, func(organizationID string) error {
		// Trailing edge fires from a timer goroutine after the request
		// context that suppressed it is gone.
		t.signal(context.Background(), organizationID)
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
		if _, ok := seen[organizationID]; ok {
			continue
		}
		seen[organizationID] = struct{}{}
		if t.throttle.Do(organizationID) {
			t.signal(ctx, organizationID)
		}
	}
}

func (t *UsageTrigger) signal(ctx context.Context, organizationID string) {
	var state GateState
	err := t.cache.Get(ctx, spendGateSnapshotKey(organizationID), &state)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return
	}
	if err != nil {
		t.logger.WarnContext(ctx, "read spend gate snapshot for usage trigger",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		return
	}

	if err := t.signaler.Signal(ctx, organizationID); err != nil {
		t.logger.WarnContext(ctx, "signal spend rule evaluation for fresh usage",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
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
