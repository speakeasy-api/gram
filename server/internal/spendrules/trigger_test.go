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
	calls []spendrules.ActorEvaluationSignal
}

func (s *usageSignalerStub) SignalActor(_ context.Context, signal spendrules.ActorEvaluationSignal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, signal)
	return nil
}

func (s *usageSignalerStub) Calls() []spendrules.ActorEvaluationSignal {
	s.mu.Lock()
	defer s.mu.Unlock()
	signals := make([]spendrules.ActorEvaluationSignal, len(s.calls))
	copy(signals, s.calls)
	return signals
}

func usageRow(organizationID, urn string) telemetry.LogParams {
	return usageRowWithIdentity(organizationID, urn, "user_ada", "ada@acme.com")
}

func usageRowWithEmail(organizationID, urn, email string) telemetry.LogParams {
	return usageRowWithUserInfo(organizationID, urn, telemetry.UserInfoByEmail(email))
}

func usageRowWithIdentity(organizationID, urn, userID, email string) telemetry.LogParams {
	return usageRowWithUserInfo(organizationID, urn, telemetry.UserInfoByIDAndEmail(userID, email))
}

func usageRowWithUserInfo(organizationID, urn string, userInfo telemetry.UserInfo) telemetry.LogParams {
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
		UserInfo:   userInfo,
		Attributes: nil,
	}
}

// writeTestGateRules puts minimal rules in the cache so the trigger sees the
// organization as having enabled spend rules.
func writeTestGateRules(t *testing.T, ctx context.Context, cacheImpl *gateCache, organizationID string) {
	t.Helper()

	writeGateRules(t, ctx, cacheImpl, organizationID, spendrules.GateRule{
		RuleURN:     "spend_rule:engineering:v1",
		RuleName:    "Engineering budget",
		Action:      spendrules.ActionBlock,
		TargetExpr:  `department_name == "Engineering"`,
		RuleExpr:    `used_pct >= warn_at_pct`,
		LimitUSD:    100,
		WarnAtPct:   80,
		WindowKind:  spendrules.WindowMonthly,
		WindowStart: time.Now().UTC().Add(-time.Hour),
		WindowEnd:   time.Now().UTC().AddDate(0, 0, 7),
	})
}

func TestUsageTriggerSignalsOrgWithGateSnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	writeTestGateRules(t, ctx, cacheImpl, "org_123")

	sig := &usageSignalerStub{}
	trigger := spendrules.NewUsageTrigger(testenv.NewLogger(t), cacheImpl, sig, time.Hour)
	t.Cleanup(func() { _ = trigger.Shutdown(context.Background()) })

	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{
		usageRow("org_123", "claude-code:otel:logs"),
	})

	// The leading-edge signal fires on a detached goroutine (signalAsync), so
	// poll rather than assert synchronously.
	require.Eventually(t, func() bool {
		return len(sig.Calls()) == 1
	}, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []spendrules.ActorEvaluationSignal{{OrganizationID: "org_123", UserID: "user_ada", Email: "ada@acme.com"}}, sig.Calls())
}

func TestUsageTriggerIgnoresIrrelevantRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	writeTestGateRules(t, ctx, cacheImpl, "org_123")

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

func TestUsageTriggerSkipsRowsWithoutUserID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	writeTestGateRules(t, ctx, cacheImpl, "org_123")

	sig := &usageSignalerStub{}
	trigger := spendrules.NewUsageTrigger(testenv.NewLogger(t), cacheImpl, sig, time.Hour)
	t.Cleanup(func() { _ = trigger.Shutdown(context.Background()) })

	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{
		usageRowWithEmail("org_123", "claude-code:otel:logs", "ada@acme.com"),
	})

	require.Empty(t, sig.Calls())
}

func TestUsageTriggerDedupesOrgsWithinBatch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	writeTestGateRules(t, ctx, cacheImpl, "org_a")
	writeTestGateRules(t, ctx, cacheImpl, "org_b")

	sig := &usageSignalerStub{}
	trigger := spendrules.NewUsageTrigger(testenv.NewLogger(t), cacheImpl, sig, time.Hour)
	t.Cleanup(func() { _ = trigger.Shutdown(context.Background()) })

	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{
		usageRow("org_a", "claude-code:otel:logs"),
		usageRow("org_a", "codex:usage:metrics"),
		usageRow("org_b", "cursor:usage:metrics"),
	})

	// Both leading-edge signals fire on detached goroutines; wait for both.
	require.Eventually(t, func() bool {
		return len(sig.Calls()) == 2
	}, 2*time.Second, 5*time.Millisecond)
	require.ElementsMatch(t, []spendrules.ActorEvaluationSignal{
		{OrganizationID: "org_a", UserID: "user_ada", Email: "ada@acme.com"},
		{OrganizationID: "org_b", UserID: "user_ada", Email: "ada@acme.com"},
	}, sig.Calls())
}

func TestUsageTriggerDedupesUserIDWithinBatch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	writeTestGateRules(t, ctx, cacheImpl, "org_123")

	sig := &usageSignalerStub{}
	trigger := spendrules.NewUsageTrigger(testenv.NewLogger(t), cacheImpl, sig, time.Hour)
	t.Cleanup(func() { _ = trigger.Shutdown(context.Background()) })

	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{
		usageRowWithIdentity("org_123", "claude-code:otel:logs", "user_ada", " Ada@Acme.com "),
		usageRowWithIdentity("org_123", "codex:usage:metrics", "user_ada", "other@acme.com"),
	})

	require.Eventually(t, func() bool {
		return len(sig.Calls()) == 1
	}, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []spendrules.ActorEvaluationSignal{{OrganizationID: "org_123", UserID: "user_ada", Email: " Ada@Acme.com "}}, sig.Calls())
}

func TestUsageTriggerThrottlesAndFlushesTrailingEdge(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheImpl := newGateCache()
	writeTestGateRules(t, ctx, cacheImpl, "org_123")

	sig := &usageSignalerStub{}
	trigger := spendrules.NewUsageTrigger(testenv.NewLogger(t), cacheImpl, sig, time.Hour)

	// Leading edge signals immediately (on a detached goroutine); the second
	// batch inside the cooldown is suppressed and left pending.
	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{usageRow("org_123", "claude-code:otel:logs")})
	trigger.OnTelemetryLogsWritten(ctx, []telemetry.LogParams{usageRow("org_123", "claude-code:otel:logs")})
	require.Eventually(t, func() bool {
		return len(sig.Calls()) == 1
	}, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []spendrules.ActorEvaluationSignal{{OrganizationID: "org_123", UserID: "user_ada", Email: "ada@acme.com"}}, sig.Calls())

	// Shutdown flushes the pending trailing signal while Temporal would
	// still be reachable.
	require.NoError(t, trigger.Shutdown(ctx))
	require.Equal(t, []spendrules.ActorEvaluationSignal{
		{OrganizationID: "org_123", UserID: "user_ada", Email: "ada@acme.com"},
		{OrganizationID: "org_123", UserID: "user_ada", Email: "ada@acme.com"},
	}, sig.Calls())
}
