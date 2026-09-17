package mcp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIssuerGateFailureReason_ToolSelectionLoad(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: %w", errToolSelectionLoad, errors.New("stored policy malformed"))
	require.Equal(t, "tool_selection_load_failed", issuerGateFailureReason(err))
}

// An admission that never reached a decision is not a withdrawn credential, so
// it must not share the label that earns a workload the invalid_token challenge.
func TestIssuerGateFailureReason_WorkloadAdmissionLoad(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("prepare workload session authorization: %w", fmt.Errorf("%w: %w", errWorkloadSessionAdmissionLoad, errors.New("connection reset")))
	require.Equal(t, "workload_admission_load_failed", issuerGateFailureReason(err))
}
