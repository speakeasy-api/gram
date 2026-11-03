package access

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

// The dashboard's agent permission editor offers exactly this list. It used
// to keep a copy; this pins the set so a runtime registry change is a
// deliberate change here too.
func TestService_ListAgentPolicyScopes_OffersExactlyTheRuntimeSafeScopes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.ListAgentPolicyScopes(ctx, &gen.ListAgentPolicyScopesPayload{})
	require.NoError(t, err)

	slugs := make([]string, 0, len(result.Scopes))
	for _, scope := range result.Scopes {
		slugs = append(slugs, scope.Slug)
		require.True(t, scope.AgentEligible, scope.Slug)
		require.Equal(t, authz.ScopeVisibilityUserVisible, scope.Visibility, scope.Slug)
		require.NotEmpty(t, scope.Label, scope.Slug)
		require.NotEmpty(t, scope.Description, scope.Slug)
		require.NotContains(t, scope.Slug, ":blocked_")
	}
	sort.Strings(slugs)
	require.Equal(t, []string{
		"assistant:read",
		"assistant:write",
		"environment:read",
		"environment:write",
		"inference:execute",
		"mcp:connect",
		"mcp:read",
		"mcp:write",
		"org:device_agent_sync",
		"org:hooks_ingest",
		"project:read",
		"project:write",
		"risk_policy:evaluate",
		"skill:read",
		"skill:write",
	}, slugs)

	// Presentation comes from the catalogue, the same as listScopes.
	full, err := ti.service.ListScopes(ctx, &gen.ListScopesPayload{})
	require.NoError(t, err)
	labels := make(map[string]string, len(full.Scopes))
	for _, scope := range full.Scopes {
		labels[scope.Slug] = scope.Label
	}
	for _, scope := range result.Scopes {
		require.Equal(t, labels[scope.Slug], scope.Label, scope.Slug)
	}
}
