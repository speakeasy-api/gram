package background

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestLegacyDrainRiskAnalysisWorkflowCompletes(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(legacyDrainRiskAnalysisWorkflow, workflow.RegisterOptions{
		Name:                          legacyDrainRiskAnalysisWorkflowName,
		DisableAlreadyRegisteredCheck: false,
	})
	env.ExecuteWorkflow(legacyDrainRiskAnalysisWorkflowName, legacyDrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
		MaxMessages:  100,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
