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
