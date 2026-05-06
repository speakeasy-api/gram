package risk_test

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// instrumentedPIIScanner records concurrency observed during AnalyzeBatch and
// (optionally) returns a finding so tests can simulate "fast match" policies.
//
//   - delay        — sleep before returning, exits early on ctx cancel.
//   - findOnEntity — if non-empty AND the policy's entities slice contains it,
//     AnalyzeBatch returns a single Finding immediately (no sleep).
//     Used to differentiate fast-matching vs slow-no-match policies.
type instrumentedPIIScanner struct {
	delay        time.Duration
	findOnEntity string

	callCount     atomic.Int32
	inflight      atomic.Int32
	maxInflight   atomic.Int32
	cancellations atomic.Int32
}

func (l *instrumentedPIIScanner) AnalyzeBatch(ctx context.Context, texts []string, entities []string, _ func()) ([][]risk_analysis.Finding, error) {
	l.callCount.Add(1)
	cur := l.inflight.Add(1)
	defer l.inflight.Add(-1)

	for {
		prev := l.maxInflight.Load()
		if cur <= prev || l.maxInflight.CompareAndSwap(prev, cur) {
			break
		}
	}

	// Fast-match short-circuit: if this policy's entities contain the configured
	// trigger, return a finding without sleeping.
	if l.findOnEntity != "" {
		if slices.Contains(entities, l.findOnEntity) {
			out := make([][]risk_analysis.Finding, len(texts))
			for i := range texts {
				out[i] = []risk_analysis.Finding{{
					RuleID:      l.findOnEntity,
					Description: l.findOnEntity,
					Match:       "x",
				}}
			}
			return out, nil
		}
	}

	select {
	case <-time.After(l.delay):
	case <-ctx.Done():
		l.cancellations.Add(1)
		return nil, fmt.Errorf("context canceled: %w", ctx.Err())
	}

	return make([][]risk_analysis.Finding, len(texts)), nil
}

// insertPresidioBlockPolicy inserts a single enforcing policy with
// sources=[presidio] using the given entities. Sidesteps the service so the
// test exercises the scanner directly.
func insertPresidioBlockPolicy(t *testing.T, ti *testInstance, ctx context.Context, name string, entities []string) {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	require.NotNil(t, authCtx.ProjectID)
	_, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:               uuid.New(),
		ProjectID:        *authCtx.ProjectID,
		OrganizationID:   authCtx.ActiveOrganizationID,
		Name:             name,
		Sources:          []string{"presidio"},
		PresidioEntities: entities,
		Enabled:          true,
		Action:           "block",
		AutoName:         false,
	})
	require.NoError(t, err)
}

// TestScanner_FanOutAcrossPoliciesIsConcurrent verifies that
// ScanForEnforcement runs Presidio scans for distinct policies in parallel
// rather than serially. With N policies each adding `delay` of latency, a
// sequential implementation would take N*delay; the parallel implementation
// should finish in roughly one delay window.
func TestScanner_FanOutAcrossPoliciesIsConcurrent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	const n = 4
	for i := range n {
		insertPresidioBlockPolicy(t, ti, ctx, "p"+strconv.Itoa(i), []string{"EMAIL_ADDRESS"})
	}

	pii := &instrumentedPIIScanner{delay: 200 * time.Millisecond}
	scanner, err := risk.NewScanner(
		testenv.NewLogger(t),
		ti.conn,
		pii,
		testenv.NewMeterProvider(t),
	)
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	start := time.Now()
	result, err := scanner.ScanForEnforcement(ctx, *authCtx.ProjectID, "irrelevant text")
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.Nil(t, result, "no findings configured, expected nil result")
	require.Equal(t, int32(n), pii.callCount.Load(), "all policies should call AnalyzeBatch")
	require.GreaterOrEqual(t, pii.maxInflight.Load(), int32(2), "expected >=2 concurrent presidio calls; saw max=%d", pii.maxInflight.Load())

	// Sequential floor would be n * delay (= 800ms). Allow generous slack but
	// fail if we're anywhere near it.
	maxAllowed := time.Duration(n) * pii.delay / 2
	require.Less(t, elapsed, maxAllowed,
		"wall time %v >= half-of-sequential %v — fan-out not happening", elapsed, maxAllowed)
}

// TestScanner_FirstMatchCancelsSiblings verifies that once a policy returns a
// match, in-flight scans for sibling policies are cancelled instead of
// running to completion.
func TestScanner_FirstMatchCancelsSiblings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	// One fast-match policy uses the FAST entity; sibling policies use a
	// non-trigger entity and would block on the long delay.
	insertPresidioBlockPolicy(t, ti, ctx, "fast", []string{"FAST"})
	for i := range 3 {
		insertPresidioBlockPolicy(t, ti, ctx, "slow"+strconv.Itoa(i), []string{"EMAIL_ADDRESS"})
	}

	pii := &instrumentedPIIScanner{
		delay:        2 * time.Second, // long enough that any non-cancellation would dominate
		findOnEntity: "FAST",
	}
	scanner, err := risk.NewScanner(
		testenv.NewLogger(t),
		ti.conn,
		pii,
		testenv.NewMeterProvider(t),
	)
	require.NoError(t, err)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	start := time.Now()
	result, err := scanner.ScanForEnforcement(ctx, *authCtx.ProjectID, "irrelevant text")
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, result, "expected match from fast policy")
	require.Equal(t, "fast", result.PolicyName)

	// Should return well before the 2s delay if siblings were cancelled.
	require.Less(t, elapsed, 1*time.Second,
		"wall time %v suggests siblings ran to completion; expected cancellation", elapsed)

	// Give the cancelled goroutines a moment to record their ctx.Err.
	time.Sleep(100 * time.Millisecond)
	require.GreaterOrEqual(t, pii.cancellations.Load(), int32(1),
		"expected at least one slow policy to observe ctx cancellation")
}
