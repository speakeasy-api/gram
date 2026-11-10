package gram

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

// The recorder's writer must be joined before runShutdown returns and ClickHouse closes.
func TestConnectionRecorderShutdown_EndsBeforeRunShutdown(t *testing.T) {
	t.Parallel()

	require.Less(t, connectionRecorderShutdownTimeout+mcpconnectionattempts.MaxShutdownOverrun, runShutdownTimeout)
}
