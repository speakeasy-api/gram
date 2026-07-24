package spendrules_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/spendrules"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type usageSignalerStub struct {
	mu    sync.Mutex
	calls []string
}

func (s *usageSignalerStub) Signal(_ context.Context, organizationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, organizationID)
	return nil
}

func (s *usageSignalerStub) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	copy(out, s.calls)
	return out
}

func usageRow(organizationID, urn string) telemetry.LogParams {
	return telemetry.LogParams{
		Timestamp: time.Now(),
		ToolInfo: telemetry.ToolInfo{
			Name:           "claude-code",
			OrganizationID: organizationID,
			ProjectID:      "9a3c8f0e-0000-0000-0000-000000000001",
			ID:             "",
			URN:            urn,
			DeploymentID:   "",
			FunctionID:     nil,
		},
		UserInfo:   telemetry.UserInfoByEmail("ada@acme.com"),
		Attributes: nil,
	}
}

// writeTestGateSnapshot puts a minimal gate snapshot in the cache so the
// trigger sees the organization as having enabled spend rules.
func writeTestGateSnapshot(t *testing.T, ctx context.Context, cacheImpl *gateCache, organizationID string) {
	t.Helper()

	state := spendrules.NewGateState(organizationID, testActors())
	state.Rules = append(state.Rules, spendrules.GateRule{
		RuleURN:    "spend_rule:engineering:v1",
		RuleName:   "Engineering budget",
		Action:     spendrules.ActionBlock,
		TargetExpr: `department_name == "Engineering"`,
		RuleExpr:   `used_pct >= warn_at_pct`,
		LimitUSD:   100,
		WarnAtPct:  80,
		WindowKind: spendrules.WindowMonthly,
		WindowEnd:  time.Now().UTC().AddDate(0, 0, 7),
	})
	require.NoError(t, spendrules.WriteGateState(ctx, cacheImpl, organizationID, state))
}

func TestUsageTriggerSignalsOrgWithGateSnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	writeTestGateSnapshot(t, ctx, cacheImpl, "org_123")

	sig := &usageSignalerStub{}
	trigger := spendrules.NewUsageTrigger(testenv.NewLogger(t), cacheImpl, sig, time.Hour)
	t.Cleanup(func() { _ = trigger.Shutdown(context.Background()) })

	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{
		usageRow("org_123", "claude-code:otel:logs"),
	})

	require.Equal(t, []string{"org_123"}, sig.Calls())
}

func TestUsageTriggerIgnoresIrrelevantRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	writeTestGateSnapshot(t, ctx, cacheImpl, "org_123")

	sig := &usageSignalerStub{}
	trigger := spendrules.NewUsageTrigger(testenv.NewLogger(t), cacheImpl, sig, time.Hour)
	t.Cleanup(func() { _ = trigger.Shutdown(context.Background()) })

	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{
		// Not a spend-relevant URN (generic gen_ai chat rows are excluded
		// from spend_rule_usage_summaries_mv).
		usageRow("org_123", "tools:http:acme"),
		// Spend-relevant URN but no organization attribution.
		usageRow("", "claude-code:otel:logs"),
	})

	require.Empty(t, sig.Calls())
}

func TestUsageTriggerSkipsOrgWithoutGateSnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()

	sig := &usageSignalerStub{}
	trigger := spendrules.NewUsageTrigger(testenv.NewLogger(t), cacheImpl, sig, time.Hour)
	t.Cleanup(func() { _ = trigger.Shutdown(context.Background()) })

	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{
		usageRow("org_no_rules", "cursor:usage:metrics"),
	})

	require.Empty(t, sig.Calls())
}

func TestUsageTriggerDedupesOrgsWithinBatch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	writeTestGateSnapshot(t, ctx, cacheImpl, "org_a")
	writeTestGateSnapshot(t, ctx, cacheImpl, "org_b")

	sig := &usageSignalerStub{}
	trigger := spendrules.NewUsageTrigger(testenv.NewLogger(t), cacheImpl, sig, time.Hour)
	t.Cleanup(func() { _ = trigger.Shutdown(context.Background()) })

	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{
		usageRow("org_a", "claude-code:otel:logs"),
		usageRow("org_a", "codex:usage:metrics"),
		usageRow("org_b", "cursor:usage:metrics"),
	})

	require.ElementsMatch(t, []string{"org_a", "org_b"}, sig.Calls())
}

func TestUsageTriggerThrottlesAndFlushesTrailingEdge(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	writeTestGateSnapshot(t, ctx, cacheImpl, "org_123")

	sig := &usageSignalerStub{}
	trigger := spendrules.NewUsageTrigger(testenv.NewLogger(t), cacheImpl, sig, time.Hour)

	// Leading edge signals immediately; the second batch inside the cooldown
	// is suppressed and left pending.
	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{usageRow("org_123", "claude-code:otel:logs")})
	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{usageRow("org_123", "claude-code:otel:logs")})
	require.Equal(t, []string{"org_123"}, sig.Calls())

	// Shutdown flushes the pending trailing signal while Temporal would
	// still be reachable.
	require.NoError(t, trigger.Shutdown(ctx))
	require.Equal(t, []string{"org_123", "org_123"}, sig.Calls())
}
