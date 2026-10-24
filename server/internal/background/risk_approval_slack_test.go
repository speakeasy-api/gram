package background

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/risk/approval/slackapproval"
)

// riskApprovalSlackRun runs the workflow with stub activities. sent is how
// many DMs the send activity reports and expiresIn when the request lapses;
// decideAfter, when positive, signals a decision that long after the start.
// It returns how often the update activity ran and how long the run took on
// the test clock.
func riskApprovalSlackRun(t *testing.T, sent int, expiresIn, decideAfter time.Duration) (int32, time.Duration) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	started := env.Now()
	var finished atomic.Int32
	env.RegisterActivityWithOptions(
		func(context.Context, slackapproval.Target) (slackapproval.NotifyResult, error) {
			return slackapproval.NotifyResult{Sent: sent, ExpiresAt: started.Add(expiresIn)}, nil
		},
		activity.RegisterOptions{Name: "NotifyRiskApprovalApprovers"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, slackapproval.Target) error {
			finished.Add(1)
			return nil
		},
		activity.RegisterOptions{Name: "FinishRiskApprovalCards"},
	)
	if decideAfter > 0 {
		env.RegisterDelayedCallback(func() {
			env.SignalWorkflow(riskApprovalSlackDecidedSignal, nil)
		}, decideAfter)
	}
	env.ExecuteWorkflow(RiskApprovalSlackWorkflow, slackapproval.Target{OrganizationID: "<ORGANIZATION_ID>", ProjectID: uuid.New(), ApprovalID: uuid.New()})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	return finished.Load(), env.Now().Sub(started)
}

func TestRiskApprovalSlackWorkflowStopsWhenNobodyWasSentADM(t *testing.T) {
	t.Parallel()
	finished, _ := riskApprovalSlackRun(t, 0, 30*time.Minute, 0)
	require.Equal(t, int32(0), finished)
}

func TestRiskApprovalSlackWorkflowUpdatesCardsOnDecision(t *testing.T) {
	t.Parallel()
	finished, took := riskApprovalSlackRun(t, 2, time.Hour, 5*time.Minute)
	require.Equal(t, int32(1), finished)
	require.Less(t, took, 10*time.Minute, "the decision, not the expiry timer, ends the wait")
}

func TestRiskApprovalSlackWorkflowUpdatesCardsOnExpiry(t *testing.T) {
	t.Parallel()
	finished, took := riskApprovalSlackRun(t, 1, 30*time.Minute, 0)
	require.Equal(t, int32(1), finished)
	require.InDelta(t, float64(30*time.Minute+riskApprovalSlackExpiryGrace), float64(took), float64(time.Second), "the expiry timer, plus its grace, ends the wait")
}

func TestRiskApprovalSlackLastSendAttemptKeepsTheDMsItSent(t *testing.T) {
	t.Parallel()
	failed := errors.New("Slack is down")
	sent := slackapproval.NotifyResult{Sent: 2, ExpiresAt: time.Now()}
	nobody := slackapproval.NotifyResult{Sent: 0, ExpiresAt: time.Time{}}

	_, err := keepSentOnLastAttempt(sent, failed, 1)
	require.ErrorIs(t, err, failed, "an early attempt is retried")
	result, err := keepSentOnLastAttempt(sent, failed, riskApprovalSlackActivityAttempts)
	require.NoError(t, err, "the last attempt goes on with the DMs it sent")
	require.Equal(t, sent, result)
	_, err = keepSentOnLastAttempt(nobody, failed, riskApprovalSlackActivityAttempts)
	require.ErrorIs(t, err, failed, "with nothing sent, the run fails as before")
}
