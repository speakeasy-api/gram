package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/k8s"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestExecuteCustomDomainReconcileSignalsAfterRequestCancellation(t *testing.T) {
	t.Parallel()

	customDomainID := uuid.New()
	params := CustomDomainReconcileParams{CustomDomainID: customDomainID}
	workflowID := CustomDomainReconcileWorkflowID(customDomainID)
	var signalContextErr error
	var signalDeadline time.Time
	var signalHasDeadline bool
	var signalContextOK bool

	temporalClient := &temporalmocks.Client{}
	temporalClient.On(
		"SignalWithStartWorkflow",
		mock.Anything,
		workflowID,
		customDomainReconcileSignal(params),
		"reconcile",
		mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
			return options.ID == workflowID &&
				options.TaskQueue == "test" &&
				options.WorkflowRunTimeout == customDomainReconcileWorkflowRunTimeout
		}),
		mock.Anything,
		params,
	).Run(func(args mock.Arguments) {
		signalCtx, ok := args.Get(0).(context.Context)
		signalContextOK = ok
		if !ok {
			return
		}
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

	require.True(t, signalContextOK)
	require.NoError(t, signalContextErr)
	require.True(t, signalHasDeadline)
	require.WithinDuration(t, time.Now().Add(10*time.Second), signalDeadline, time.Second)
}

func TestWaitForCurrentCustomDomainReconcileRun(t *testing.T) {
	t.Parallel()

	t.Run("completed", func(t *testing.T) {
		t.Parallel()

		run := &stubWorkflowRun{}
		require.NoError(t, waitForCurrentCustomDomainReconcileRun(t.Context(), run))
		require.True(t, run.options.DisableFollowingRuns)
	})

	t.Run("continued as new", func(t *testing.T) {
		t.Parallel()

		run := &stubWorkflowRun{err: &workflow.ContinueAsNewError{}}
		require.NoError(t, waitForCurrentCustomDomainReconcileRun(t.Context(), run))
		require.True(t, run.options.DisableFollowingRuns)
	})

	t.Run("failed", func(t *testing.T) {
		t.Parallel()

		expected := errors.New("reconcile failed")
		run := &stubWorkflowRun{err: expected}
		require.ErrorIs(t, waitForCurrentCustomDomainReconcileRun(t.Context(), run), expected)
		require.True(t, run.options.DisableFollowingRuns)
	})
}

func TestExecuteCustomDomainRegistrationUsesRegistrationBudget(t *testing.T) {
	t.Parallel()

	temporalClient := &temporalmocks.Client{}
	temporalClient.On(
		"ExecuteWorkflow",
		mock.Anything,
		mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
			return options.WorkflowRunTimeout == customDomainRegistrationWorkflowRunTimeout
		}),
		mock.Anything,
		mock.Anything,
	).Return(nil, nil).Once()

	_, err := (&CustomDomainRegistrationClient{
		TemporalEnv: tenv.NewEnvironment(temporalClient, "test", "test"),
	}).ExecuteCustomDomainRegistration(
		t.Context(),
		"test-organization",
		"test.example.com",
		urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		nil,
		k8s.ProvisionerKindIngress,
		nil,
	)
	require.NoError(t, err)
	temporalClient.AssertExpectations(t)
}

type stubWorkflowRun struct {
	err     error
	options client.WorkflowRunGetOptions
}

func (s *stubWorkflowRun) Get(context.Context, any) error {
	return errors.New("unexpected WorkflowRun.Get call")
}

func (s *stubWorkflowRun) GetWithOptions(_ context.Context, _ any, options client.WorkflowRunGetOptions) error {
	s.options = options
	return s.err
}

func (s *stubWorkflowRun) GetID() string {
	return "workflow"
}

func (s *stubWorkflowRun) GetRunID() string {
	return "run"
}
