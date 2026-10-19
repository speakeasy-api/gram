package testenv

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/api/workflowservice/v1"
)

func TestTemporalEnvironmentConcurrentRegistration(t *testing.T) {
	t.Parallel()

	// namespaceCount exceeds the server's default 40-request burst so this
	// exercises the registration pressure from a package full of parallel tests.
	const namespaceCount = 128
	for i := range namespaceCount {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()

			env, _ := infra.NewTemporalEnv(t)
			namespace, err := env.Client().WorkflowService().DescribeNamespace(t.Context(), &workflowservice.DescribeNamespaceRequest{
				Namespace: string(env.Namespace()),
			})
			require.NoError(t, err)
			require.Equal(t, string(env.Namespace()), namespace.GetNamespaceInfo().GetName())
		})
	}
}
