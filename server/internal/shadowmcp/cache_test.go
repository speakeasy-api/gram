package shadowmcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestIsEnabledForProject_NilProjectID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	assert.False(t, f.client.IsEnabledForProject(t.Context(), uuid.Nil))
}

func TestIsEnabledForProject_NoPolicy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	assert.False(t, f.client.IsEnabledForProject(t.Context(), f.projectID))
}

func TestIsEnabledForProject_NonShadowMCPSourceIgnored(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.createPolicy(t, "gitleaks-only", true, []string{"gitleaks"})

	assert.False(t, f.client.IsEnabledForProject(t.Context(), f.projectID))
}

func TestIsEnabledForProject_EnabledDestructiveToolPolicy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.createPolicy(t, "destructive-tool", true, []string{"destructive_tool"})

	require.True(t, f.client.IsEnabledForProject(t.Context(), f.projectID))
}

func TestIsEnabledForProject_DisabledDestructiveToolPolicyIgnored(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.createPolicy(t, "destructive-tool", false, []string{"destructive_tool"})

	assert.False(t, f.client.IsEnabledForProject(t.Context(), f.projectID))
}

func TestIsEnabledForProject_DisabledShadowMCPPolicyIgnored(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.createPolicy(t, "disabled", false, []string{"shadow_mcp"})

	assert.False(t, f.client.IsEnabledForProject(t.Context(), f.projectID))
}

func TestIsEnabledForProject_EnabledShadowMCPPolicy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.createPolicy(t, "enabled", true, []string{"shadow_mcp"})

	require.True(t, f.client.IsEnabledForProject(t.Context(), f.projectID))
}

func TestIsEnabledForProject_CachesResult(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.createPolicy(t, "enabled", true, []string{"shadow_mcp"})

	assert.True(t, f.client.IsEnabledForProject(t.Context(), f.projectID))

	// Wipe risk_policies behind the cache. If the cache is honored the
	// answer should remain true; otherwise the second lookup would hit
	// the now-empty table and return false.
	require.NoError(t, riskrepo.New(f.conn).HardDeleteRiskPoliciesByProject(t.Context(), f.projectID))

	assert.True(t, f.client.IsEnabledForProject(t.Context(), f.projectID))
}

func TestInvalidate_DropsCachedAnswer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.createPolicy(t, "enabled", true, []string{"shadow_mcp"})

	require.True(t, f.client.IsEnabledForProject(t.Context(), f.projectID))

	require.NoError(t, riskrepo.New(f.conn).HardDeleteRiskPoliciesByProject(t.Context(), f.projectID))

	f.client.Invalidate(t.Context(), f.projectID)

	assert.False(t, f.client.IsEnabledForProject(t.Context(), f.projectID))
}
