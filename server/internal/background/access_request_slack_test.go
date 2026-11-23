package background

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/access/slackrequests"
)

// accessRequestSlackRun runs the workflow with stub activities. sent is how
// many DMs the send activity reports and expiresIn when the request lapses;
// decideAfter, when positive, signals a decision that long after the start.
// It returns how often the update activity ran and how long the run took on
// the test clock.
func accessRequestSlackRun(t *testing.T, sent int, expiresIn, decideAfter time.Duration) (int32, time.Duration) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	started := env.Now()
	var finished atomic.Int32
	env.RegisterActivityWithOptions(
		func(context.Context, slackrequests.Target) (slackrequests.NotifyResult, error) {
			return slackrequests.NotifyResult{Sent: sent, ExpiresAt: started.Add(expiresIn)}, nil
		},
		activity.RegisterOptions{Name: "NotifyAccessRequestAdmins"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, slackrequests.Target) error {
			finished.Add(1)
			return nil
		},
		activity.RegisterOptions{Name: "FinishAccessRequestCards"},
	)
	if decideAfter > 0 {
		env.RegisterDelayedCallback(func() {
			env.SignalWorkflow(accessRequestSlackDecidedSignal, nil)
		}, decideAfter)
	}
	env.ExecuteWorkflow(AccessRequestSlackWorkflow, slackrequests.Target{OrganizationID: "<ORGANIZATION_ID>", RequestID: uuid.New()})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	return finished.Load(), env.Now().Sub(started)
}

func TestAccessRequestSlackWorkflowStopsWhenNobodyWasSentADM(t *testing.T) {
	t.Parallel()
	finished, _ := accessRequestSlackRun(t, 0, 30*time.Minute, 0)
	require.Equal(t, int32(0), finished)
}

func TestAccessRequestSlackWorkflowUpdatesCardsOnDecision(t *testing.T) {
	t.Parallel()
	finished, took := accessRequestSlackRun(t, 2, time.Hour, 5*time.Minute)
	require.Equal(t, int32(1), finished)
	require.Less(t, took, 10*time.Minute, "the decision, not the expiry timer, ends the wait")
}

func TestAccessRequestSlackWorkflowUpdatesCardsOnExpiry(t *testing.T) {
	t.Parallel()
	finished, took := accessRequestSlackRun(t, 1, 30*time.Minute, 0)
	require.Equal(t, int32(1), finished)
	require.InDelta(t, float64(30*time.Minute+accessRequestSlackExpiryGrace), float64(took), float64(time.Second), "the expiry timer, plus its grace, ends the wait")
}

func TestAccessRequestSlackLastSendAttemptKeepsTheDMsItSent(t *testing.T) {
	t.Parallel()
	sent := slackrequests.NotifyResult{Sent: 2, ExpiresAt: time.Now()}
	nobody := slackrequests.NotifyResult{Sent: 0, ExpiresAt: time.Time{}}

	require.False(t, keepsSentAccessRequestDMs(sent, 1), "an early attempt is retried")
	require.True(t, keepsSentAccessRequestDMs(sent, accessRequestSlackActivityAttempts), "the last attempt goes on with the DMs sent")
	require.False(t, keepsSentAccessRequestDMs(nobody, accessRequestSlackActivityAttempts), "with nothing sent, the run fails as before")
}
