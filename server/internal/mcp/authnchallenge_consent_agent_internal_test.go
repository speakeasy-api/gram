package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAgentAuthorizationTargetRejectsMetaEndpoint(t *testing.T) {
	t.Parallel()

	endpoint := &ResolvedMcpEndpoint{
		MetaMcpServerID:     uuid.NullUUID{UUID: uuid.New(), Valid: true},
		OrganizationID:      "org-placeholder",
		ProjectID:           uuid.New(),
		UserSessionIssuerID: uuid.New(),
	}

	target, ok := agentAuthorizationTarget(endpoint)
	require.False(t, ok)
	require.Nil(t, target)
}
