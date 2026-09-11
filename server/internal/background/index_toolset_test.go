package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestExecuteIndexToolsetWithoutTemporal(t *testing.T) {
	t.Parallel()

	run, err := ExecuteIndexToolset(t.Context(), nil, IndexToolsetParams{
		ProjectID:             uuid.New(),
		ToolsetID:             uuid.New(),
		ToolsetSlug:           types.Slug("unavailable-index"),
		ToolsetVersion:        1,
		DeploymentID:          uuid.New(),
		PermanentFailureCount: 0,
	})
	require.ErrorIs(t, err, ErrTemporalUnavailable)
	require.Nil(t, run)
}

func TestIndexToolsetWorkflow_PermanentFailureCoolsDownThenRetries(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
	attempts := 0
	env.RegisterActivityWithOptions(
		func(context.Context, activities.GenerateToolsetEmbeddingsInput) error {
			attempts++
			if attempts == 1 {
				return temporal.NewNonRetryableApplicationError(
					"provider rejected request",
					activities.GenerateToolsetEmbeddingsPermanentErrorType,
					nil,
				)
			}
			return nil
		},
		activity.RegisterOptions{Name: "GenerateToolsetEmbeddings"},
	)

	env.ExecuteWorkflow(IndexToolsetWorkflow, IndexToolsetParams{
		ProjectID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27"),
		ToolsetID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e26"),
		ToolsetSlug:           types.Slug("test-toolset"),
		ToolsetVersion:        1,
		DeploymentID:          uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e28"),
		PermanentFailureCount: 0,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, attempts)
}

func TestIndexToolsetWorkflow_TransientFailureUsesBoundedActivityRetry(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
	attempts := 0
	env.RegisterActivityWithOptions(
		func(context.Context, activities.GenerateToolsetEmbeddingsInput) error {
			attempts++
			return errors.New("provider unavailable")
		},
		activity.RegisterOptions{Name: "GenerateToolsetEmbeddings"},
	)

	env.ExecuteWorkflow(IndexToolsetWorkflow, IndexToolsetParams{
		ProjectID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27"),
		ToolsetID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e26"),
		ToolsetSlug:           types.Slug("test-toolset"),
		ToolsetVersion:        1,
		DeploymentID:          uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e28"),
		PermanentFailureCount: 0,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, 2, attempts)
}

func TestIndexToolsetWorkflow_PermanentFailureIsSuppressedAtLimit(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
	attempts := 0
	env.RegisterActivityWithOptions(
		func(context.Context, activities.GenerateToolsetEmbeddingsInput) error {
			attempts++
			return temporal.NewNonRetryableApplicationError(
				"provider rejected request",
				activities.GenerateToolsetEmbeddingsPermanentErrorType,
				nil,
			)
		},
		activity.RegisterOptions{Name: "GenerateToolsetEmbeddings"},
	)

	env.ExecuteWorkflow(IndexToolsetWorkflow, IndexToolsetParams{
		ProjectID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27"),
		ToolsetID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e26"),
		ToolsetSlug:           types.Slug("test-toolset"),
		ToolsetVersion:        1,
		DeploymentID:          uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e28"),
		PermanentFailureCount: 0,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, indexToolsetPermanentFailureLimit, attempts)
}

func TestIndexToolsetWorkflow_SupersededRevisionCompletes(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(
		func(context.Context, activities.GenerateToolsetEmbeddingsInput) error {
			return temporal.NewNonRetryableApplicationError(
				"revision changed",
				activities.GenerateToolsetEmbeddingsSupersededErrorType,
				nil,
			)
		},
		activity.RegisterOptions{Name: "GenerateToolsetEmbeddings"},
	)

	env.ExecuteWorkflow(IndexToolsetWorkflow, IndexToolsetParams{
		ProjectID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27"),
		ToolsetID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e26"),
		ToolsetSlug:           types.Slug("test-toolset"),
		ToolsetVersion:        1,
		DeploymentID:          uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e28"),
		PermanentFailureCount: 0,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestIndexToolsetWorkflowIDIncludesRevision(t *testing.T) {
	t.Parallel()

	base := IndexToolsetParams{
		ProjectID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27"),
		ToolsetID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e26"),
		ToolsetSlug:           types.Slug("test-toolset"),
		ToolsetVersion:        1,
		DeploymentID:          uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e28"),
		PermanentFailureCount: 0,
	}
	next := base
	next.ToolsetVersion = 2
	next.DeploymentID = uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e29")

	require.NotEqual(t, indexToolsetWorkflowID(base), indexToolsetWorkflowID(next))
	recreated := base
	recreated.ToolsetID = uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e25")
	require.NotEqual(t, indexToolsetWorkflowID(base), indexToolsetWorkflowID(recreated))
}

func TestIndexToolsetPermanentRetryDelayIsCappedAndStable(t *testing.T) {
	t.Parallel()

	params := IndexToolsetParams{
		ProjectID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27"),
		ToolsetID:             uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e26"),
		ToolsetSlug:           types.Slug("test-toolset"),
		ToolsetVersion:        1,
		DeploymentID:          uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e28"),
		PermanentFailureCount: 1,
	}
	first := indexToolsetPermanentRetryDelay(params)
	require.GreaterOrEqual(t, first, indexToolsetPermanentRetryInitial)
	require.Less(t, first, 2*indexToolsetPermanentRetryInitial)
	require.Equal(t, first, indexToolsetPermanentRetryDelay(params))

	params.PermanentFailureCount = 100
	require.Equal(t, indexToolsetPermanentRetryMaximum, indexToolsetPermanentRetryDelay(params))
}
