package access

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
}

// An owner configuring their own agent holds no organization grant, so the
// list must not need one.
func TestService_ListAgentPolicyScopes_NeedsNoOrganizationGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.ListAgentPolicyScopes(authztest.WithExactGrants(t, ctx), &gen.ListAgentPolicyScopesPayload{})
	require.NoError(t, err)
	require.NotEmpty(t, result.Scopes)
}

func TestService_ListAgentPolicyScopes_Unauthorized(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = contextvalues.SetAuthContext(ctx, nil)

	_, err := ti.service.ListAgentPolicyScopes(ctx, &gen.ListAgentPolicyScopesPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing auth context")
}
