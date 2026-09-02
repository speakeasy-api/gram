package temporalreply

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestWorkflowFirstReplyWins(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ReplySignalName, []byte("first"))
		env.SignalWorkflow(ReplySignalName, []byte("duplicate"))
	}, 0)

	env.ExecuteWorkflow(Workflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var got []byte
	require.NoError(t, env.GetWorkflowResult(&got))
	require.Equal(t, []byte("first"), got)
}

func TestWorkflowWaitsForReply(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ReplySignalName, []byte("later"))
	}, time.Second)

	env.ExecuteWorkflow(Workflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var got []byte
	require.NoError(t, env.GetWorkflowResult(&got))
	require.Equal(t, []byte("later"), got)
}
