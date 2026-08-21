package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// deadlineRecordingScanner captures the deadline the gating path hands to
// ScanForEnforcement, and optionally waits for that deadline before returning
// the result the real scanner would produce.
type deadlineRecordingScanner struct {
	// waitForDeadline blocks the scan until the supplied context is done, so a
	// test can observe what the gating path does with an expired scan.
	waitForDeadline bool

	// result is returned once the scan finishes. Non-nil models a policy that
	// matched — including a fail-closed prompt policy, which turns a judge
	// timeout into a block rather than an error.
	result *risk.ScanResult

	// err is returned once the scan finishes, alongside a nil result.
	err error

	// observedDeadline is the deadline seen on the scan context, and
	// hasDeadline reports whether one was set at all.
	observedDeadline time.Time
	hasDeadline      bool

	// observedCtxErr is the scan context's error at the moment the scan
	// returned.
	observedCtxErr error
}

func (s *deadlineRecordingScanner) ScanForEnforcement(ctx context.Context, _ string, _ uuid.UUID, _ string, _ string, _ message.Type, _ string) (*risk.ScanResult, error) {
	s.observedDeadline, s.hasDeadline = ctx.Deadline()
	if s.waitForDeadline {
		<-ctx.Done()
	}
	s.observedCtxErr = ctx.Err()
	return s.result, s.err
}

func (s *deadlineRecordingScanner) LookupShadowMCPBlockingPolicy(context.Context, string, uuid.UUID, string) (*risk.ShadowMCPPolicy, error) {
	return nil, nil
}

func (s *deadlineRecordingScanner) HasEnabledShadowMCPPolicy(context.Context, uuid.UUID) (bool, error) {
	return false, nil
}

func (s *deadlineRecordingScanner) HasAcknowledgedChallenge(context.Context, uuid.UUID, string, string, string, string) bool {
	return false
}

func (s *deadlineRecordingScanner) RecordPolicyChallenge(context.Context, string, uuid.UUID, string, string, string, string, string, string, string) {
}

// newGatingScanService builds the minimal Service the gating scan path needs.
// It deliberately avoids a struct literal so the test does not have to name
// every unrelated Service dependency.
func newGatingScanService(t *testing.T, scanner risk.RiskScanner, timeout time.Duration) *Service {
	t.Helper()

	svc := new(Service)
	svc.logger = testenv.NewLogger(t)
	svc.riskScanner = scanner
	svc.gatingScanTimeout = timeout
	return svc
}

func newGatingScanEvent() hookevents.Event {
	return hookevents.Event{
		Provider:     hookevents.ProviderClaude,
		Type:         hookevents.EventTypeUserPromptSubmit,
		RawEventType: "UserPromptSubmit",
		Timestamp:    time.Time{},
		AuthContext:  nil,
		Context: hookevents.EventContext{
			OrganizationID: "org-1",
			ProjectID:      uuid.New(),
			User:           hookevents.User{ID: "user-1"},
		},
		ConversationID: "conv-1",
		Raw:            nil,
	}
}

func TestScanHookEventForEnforcementAppliesDefaultDeadline(t *testing.T) {
	t.Parallel()

	scanner := &deadlineRecordingScanner{waitForDeadline: false, result: nil, err: nil, observedDeadline: time.Time{}, hasDeadline: false, observedCtxErr: nil}
	svc := newGatingScanService(t, scanner, 0)

	start := time.Now()
	result := svc.scanHookEventForEnforcement(t.Context(), newGatingScanEvent(), "delete prod", message.User, "")

	require.Nil(t, result)
	require.True(t, scanner.hasDeadline, "gating scan must run under a deadline")
	remaining := scanner.observedDeadline.Sub(start)
	require.Greater(t, remaining, defaultGatingScanTimeout-time.Second)
	require.LessOrEqual(t, remaining, defaultGatingScanTimeout+time.Second)
}

func TestScanHookEventForEnforcementAppliesConfiguredDeadline(t *testing.T) {
	t.Parallel()

	scanner := &deadlineRecordingScanner{waitForDeadline: false, result: nil, err: nil, observedDeadline: time.Time{}, hasDeadline: false, observedCtxErr: nil}
	svc := newGatingScanService(t, scanner, 250*time.Millisecond)

	start := time.Now()
	result := svc.scanHookEventForEnforcement(t.Context(), newGatingScanEvent(), "delete prod", message.User, "")

	require.Nil(t, result)
	require.True(t, scanner.hasDeadline)
	remaining := scanner.observedDeadline.Sub(start)
	require.Greater(t, remaining, time.Duration(0))
	require.Less(t, remaining, time.Second, "the configured deadline must override the default")
}

// A scan that outlives the deadline must not take the surrounding hook request
// down with it: the handler still has a response to write.
func TestScanHookEventForEnforcementDeadlineLeavesCallerContextLive(t *testing.T) {
	t.Parallel()

	scanner := &deadlineRecordingScanner{waitForDeadline: true, result: nil, err: context.DeadlineExceeded, observedDeadline: time.Time{}, hasDeadline: false, observedCtxErr: nil}
	svc := newGatingScanService(t, scanner, 50*time.Millisecond)

	ctx := t.Context()
	result := svc.scanHookEventForEnforcement(ctx, newGatingScanEvent(), "delete prod", message.User, "")

	require.Nil(t, result, "a scan error allows the event through")
	require.ErrorIs(t, scanner.observedCtxErr, context.DeadlineExceeded)
	require.NoError(t, ctx.Err(), "the deadline must not cancel the caller's context")
}

// The deadline is propagated into the scan rather than enforced around it, so
// a fail-closed policy that turns the expiry into a block still has its block
// honoured. Enforcing the deadline outside the scanner would drop this result
// and allow the prompt.
func TestScanHookEventForEnforcementDeadlineHonoursFailClosedBlock(t *testing.T) {
	t.Parallel()

	blocked := &risk.ScanResult{
		Action:           "block",
		PolicyID:         uuid.New().String(),
		PolicyName:       "no prod deletes",
		Source:           "prompt_policy",
		MessageType:      message.User,
		RuleID:           "prompt_policy.match",
		Description:      "Policy judge was unavailable; flagged by fail-closed policy.",
		UserMessage:      nil,
		MatchedValue:     "",
		Entity:           "prompt_policy.match",
		CallFingerprint:  "",
		DeadLetterReason: "",
	}
	scanner := &deadlineRecordingScanner{waitForDeadline: true, result: blocked, err: nil, observedDeadline: time.Time{}, hasDeadline: false, observedCtxErr: nil}
	svc := newGatingScanService(t, scanner, 50*time.Millisecond)

	result := svc.scanHookEventForEnforcement(t.Context(), newGatingScanEvent(), "delete prod", message.User, "")

	require.NotNil(t, result, "a fail-closed block produced at the deadline must still enforce")
	require.Equal(t, "block", result.Action)
	require.ErrorIs(t, scanner.observedCtxErr, context.DeadlineExceeded)
}
