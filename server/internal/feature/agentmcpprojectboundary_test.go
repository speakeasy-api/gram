package feature_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestAgentMCPProjectBoundaryEnforced(t *testing.T) {
	t.Parallel()

	flags := new(feature.InMemory)
	require.False(t, feature.AgentMCPProjectBoundaryEnforced(t.Context(), nil, "org_a"))
	require.False(t, feature.AgentMCPProjectBoundaryEnforced(t.Context(), flags, "org_a"))

	flags.SetFlag(feature.FlagAgentMCPProjectBoundaryEnforcement, "org_a", true)
	require.True(t, feature.AgentMCPProjectBoundaryEnforced(t.Context(), flags, "org_a"))
	require.False(t, feature.AgentMCPProjectBoundaryEnforced(t.Context(), flags, "org_b"))
}
