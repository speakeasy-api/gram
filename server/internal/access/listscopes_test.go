package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestService_ListScopes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.ListScopes(ctx, &gen.ListScopesPayload{})
	require.NoError(t, err)
	require.Len(t, result.Scopes, 22)

	bySlug := make(map[string]*gen.ScopeDefinition, len(result.Scopes))
	for _, scope := range result.Scopes {
		bySlug[scope.Slug] = scope
	}

	require.Equal(t, "org", bySlug[string(authz.ScopeOrgRead)].ResourceType)
	require.Equal(t, "project", bySlug[string(authz.ScopeProjectWrite)].ResourceType)
	require.Equal(t, "mcp", bySlug[string(authz.ScopeMCPConnect)].ResourceType)
	require.Equal(t, "environment", bySlug[string(authz.ScopeEnvironmentRead)].ResourceType)
	require.Equal(t, "environment", bySlug[string(authz.ScopeEnvironmentWrite)].ResourceType)
	require.Equal(t, "risk_policy", bySlug[string(authz.ScopeRiskPolicyEvaluate)].ResourceType)
	require.Equal(t, "risk_policy", bySlug[string(authz.ScopeRiskPolicyBypass)].ResourceType)
	require.Equal(t, "chat", bySlug[string(authz.ScopeChatRead)].ResourceType)
	require.Equal(t, "org", bySlug[string(authz.ScopeOrgManageRoles)].ResourceType)
	require.Equal(t, authz.ScopeVisibilityUserVisible, bySlug[string(authz.ScopeOrgManageRoles)].Visibility)
	require.Nil(t, bySlug[string(authz.ScopeOrgManageRoles)].ExclusionScope)
	require.Equal(t, "Read organization metadata and members.", bySlug[string(authz.ScopeOrgRead)].Description)
	require.Equal(t, authz.ScopeVisibilityUserVisible, bySlug[string(authz.ScopeProjectWrite)].Visibility)
	require.Equal(t, authz.ScopeVisibilityInternal, bySlug[string(authz.ScopeProjectBlockedWrite)].Visibility)
	require.NotNil(t, bySlug[string(authz.ScopeProjectWrite)].ExclusionScope)
	require.Equal(t, string(authz.ScopeProjectBlockedWrite), *bySlug[string(authz.ScopeProjectWrite)].ExclusionScope)
	require.Nil(t, bySlug[string(authz.ScopeProjectBlockedWrite)].ExclusionScope)
	require.NotNil(t, bySlug[string(authz.ScopeMCPConnect)].ExclusionScope)
	require.Equal(t, string(authz.ScopeMCPBlockedConnect), *bySlug[string(authz.ScopeMCPConnect)].ExclusionScope)
	require.Nil(t, bySlug[string(authz.ScopeMCPBlockedConnect)].ExclusionScope)
	require.NotNil(t, bySlug[string(authz.ScopeRiskPolicyEvaluate)].ExclusionScope)
	require.Equal(t, string(authz.ScopeRiskPolicyBypass), *bySlug[string(authz.ScopeRiskPolicyEvaluate)].ExclusionScope)
	require.Nil(t, bySlug[string(authz.ScopeRiskPolicyBypass)].ExclusionScope)
}

func TestService_ListScopes_Unauthorized(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = contextvalues.SetAuthContext(ctx, nil)

	_, err := ti.service.ListScopes(ctx, &gen.ListScopesPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing auth context")
}
