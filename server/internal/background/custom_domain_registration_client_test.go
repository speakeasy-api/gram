package background

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

func TestExecuteCustomDomainReconcileSignalsAfterRequestCancellation(t *testing.T) {
	t.Parallel()

	customDomainID := uuid.New()
	params := CustomDomainReconcileParams{CustomDomainID: customDomainID}
	workflowID := CustomDomainReconcileWorkflowID(customDomainID)
	var signalContextErr error
	var signalDeadline time.Time
	var signalHasDeadline bool

	temporalClient := &temporalmocks.Client{}
	temporalClient.On(
		"SignalWithStartWorkflow",
		mock.Anything,
		workflowID,
		customDomainReconcileSignal(params),
		"reconcile",
		mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
			return options.ID == workflowID && options.TaskQueue == "test"
		}),
		mock.Anything,
		params,
	).Run(func(args mock.Arguments) {
		signalCtx := args.Get(0).(context.Context)
		signalContextErr = signalCtx.Err()
		signalDeadline, signalHasDeadline = signalCtx.Deadline()
	}).Return(nil, nil).Once()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&CustomDomainRegistrationClient{
		TemporalEnv: tenv.NewEnvironment(temporalClient, "test", "test"),
	}).ExecuteCustomDomainReconcile(ctx, customDomainID)
	require.NoError(t, err)
	temporalClient.AssertExpectations(t)

	require.NoError(t, signalContextErr)
	require.True(t, signalHasDeadline)
	require.WithinDuration(t, time.Now().Add(10*time.Second), signalDeadline, time.Second)
}
