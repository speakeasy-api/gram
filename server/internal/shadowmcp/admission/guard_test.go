package admission

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApprovalRequiredErrorCarriesTheRefusedTarget(t *testing.T) {
	t.Parallel()

	err := error(&ApprovalRequiredError{CanonicalURL: "https://mcp.example.test/server"})
	require.ErrorIs(t, err, ErrApprovalRequired)

	var typed *ApprovalRequiredError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, "https://mcp.example.test/server", typed.CanonicalURL)
}

// Compile-time assertions keep the transaction-facing guard API stable for the
// independently wired service packages.
var (
	_ = (*Guard).CheckAttachment
	_ = (*Guard).CheckPluginAudience
	_ = (*Guard).CheckMCPServerTarget
	_ = (*Guard).CheckRemoteTarget
)
