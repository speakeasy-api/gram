package interceptors

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/interceptor"
)

// These tests verify that the package's exported interceptors satisfy
// Temporal's WorkerInterceptor interface and can be constructed safely.
// Behavioral tests for InjectExecutionInfo and Recovery require a Temporal
// worker harness (see go.temporal.io/sdk/testsuite) and live under the
// background package's integration tests.

func TestRecovery_ImplementsWorkerInterceptor(t *testing.T) {
	t.Parallel()

	var _ interceptor.WorkerInterceptor = (*Recovery)(nil)
	r := &Recovery{}
	require.NotNil(t, r)
}

func TestInjectExecutionInfo_ImplementsWorkerInterceptor(t *testing.T) {
	t.Parallel()

	var _ interceptor.WorkerInterceptor = (*InjectExecutionInfo)(nil)
	i := &InjectExecutionInfo{}
	require.NotNil(t, i)
}
