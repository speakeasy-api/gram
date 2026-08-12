package assistants

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
)

func TestMCPAuthAttemptID(t *testing.T) {
	t.Parallel()

	claims := new(assistanttokens.MCPAuthFlowClaims)
	claims.FlowID = "stable-callback"
	claims.ID = "unique-attempt"
	require.Equal(t, "unique-attempt", mcpAuthAttemptID(claims))

	claims.ID = ""
	require.Equal(t, "stable-callback", mcpAuthAttemptID(claims))
}
