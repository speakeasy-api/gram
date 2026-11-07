package background

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/risk/approval/slackapproval"
)

// riskApprovalSlackStubs are what the stub activities answer. Times are
// offsets from the start of the run.
type riskApprovalSlackStubs struct {
	// sendErr fails every send attempt. Without it, the send reports sent.
	sendErr error

	// sent is how many DMs the send reports, or counting finds after a failed
	// send.
	sent int

	// expiresIn is when the request lapses.
	expiresIn time.Duration

	// decideAfter, when positive, signals a decision that long after the
	// start.
	decideAfter time.Duration
}

// riskApprovalSlackOutcome is what a test run did.
type riskApprovalSlackOutcome struct {
	// sendAttempts counts the send activity's attempts.
	sendAttempts int32

	// finished counts the update activity's runs.
	finished int32

	// took is how long the run took on the test clock.
	took time.Duration

	// err is the run's error.
	err error
}

// riskApprovalSlackRun runs the workflow with stub activities.
func riskApprovalSlackRun(t *testing.T, stubs riskApprovalSlackStubs) riskApprovalSlackOutcome {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	started := env.Now()
	result := slackapproval.NotifyResult{Sent: stubs.sent, ExpiresAt: started.Add(stubs.expiresIn)}
	var sendAttempts, finished atomic.Int32
	env.RegisterActivityWithOptions(
		func(context.Context, slackapproval.Target) (slackapproval.NotifyResult, error) {
			sendAttempts.Add(1)
			if stubs.sendErr != nil {
				return slackapproval.NotifyResult{Sent: 0, ExpiresAt: time.Time{}}, stubs.sendErr
			}
			return result, nil
		},
		activity.RegisterOptions{Name: "NotifyRiskApprovalApprovers"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, slackapproval.Target) (slackapproval.NotifyResult, error) {
			return result, nil
		},
		activity.RegisterOptions{Name: "CountRiskApprovalDMs"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, slackapproval.Target) error {
			finished.Add(1)
			return nil
		},
		activity.RegisterOptions{Name: "FinishRiskApprovalCards"},
	)
	if stubs.decideAfter > 0 {
		env.RegisterDelayedCallback(func() {
			env.SignalWorkflow(riskApprovalSlackDecidedSignal, nil)
		}, stubs.decideAfter)
	}
	env.ExecuteWorkflow(RiskApprovalSlackWorkflow, slackapproval.Target{OrganizationID: "<ORGANIZATION_ID>", ProjectID: uuid.New(), ApprovalID: uuid.New()})
	require.True(t, env.IsWorkflowCompleted())
	return riskApprovalSlackOutcome{sendAttempts: sendAttempts.Load(), finished: finished.Load(), took: env.Now().Sub(started), err: env.GetWorkflowError()}
}

func TestRiskApprovalSlackWorkflowStopsWhenNobodyWasSentADM(t *testing.T) {
	t.Parallel()
	outcome := riskApprovalSlackRun(t, riskApprovalSlackStubs{sendErr: nil, sent: 0, expiresIn: 30 * time.Minute, decideAfter: 0})
	require.NoError(t, outcome.err)
	require.Equal(t, int32(0), outcome.finished)
}

func TestRiskApprovalSlackWorkflowUpdatesCardsOnDecision(t *testing.T) {
	t.Parallel()
	outcome := riskApprovalSlackRun(t, riskApprovalSlackStubs{sendErr: nil, sent: 2, expiresIn: time.Hour, decideAfter: 5 * time.Minute})
	require.NoError(t, outcome.err)
	require.Equal(t, int32(1), outcome.finished)
	require.Less(t, outcome.took, 10*time.Minute, "the decision, not the expiry timer, ends the wait")
}

func TestRiskApprovalSlackWorkflowUpdatesCardsOnExpiry(t *testing.T) {
	t.Parallel()
	outcome := riskApprovalSlackRun(t, riskApprovalSlackStubs{sendErr: nil, sent: 1, expiresIn: 30 * time.Minute, decideAfter: 0})
	require.NoError(t, outcome.err)
	require.Equal(t, int32(1), outcome.finished)
	require.InDelta(t, float64(30*time.Minute+riskApprovalSlackExpiryGrace), float64(outcome.took), float64(time.Second), "the expiry timer, plus its grace, ends the wait")
}

func TestRiskApprovalSlackWorkflowUpdatesCardsWhenTheLastSendAttemptTimesOut(t *testing.T) {
	t.Parallel()
	timedOut := temporal.NewTimeoutError(enums.TIMEOUT_TYPE_START_TO_CLOSE, nil)
	outcome := riskApprovalSlackRun(t, riskApprovalSlackStubs{sendErr: timedOut, sent: 2, expiresIn: 2 * time.Hour, decideAfter: time.Hour})
	require.NoError(t, outcome.err)
	require.Equal(t, int32(riskApprovalSlackActivityAttempts), outcome.sendAttempts)
	require.Equal(t, int32(1), outcome.finished, "the DMs earlier attempts recorded still get the outcome")
}

func TestRiskApprovalSlackWorkflowFailsWhenNoSendAttemptRecordedADM(t *testing.T) {
	t.Parallel()
	timedOut := temporal.NewTimeoutError(enums.TIMEOUT_TYPE_START_TO_CLOSE, nil)
	outcome := riskApprovalSlackRun(t, riskApprovalSlackStubs{sendErr: timedOut, sent: 0, expiresIn: 2 * time.Hour, decideAfter: 0})
	require.Error(t, outcome.err)
	require.Equal(t, int32(0), outcome.finished)
}
