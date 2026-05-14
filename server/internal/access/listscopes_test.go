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
	require.Len(t, result.Scopes, 9)

	bySlug := make(map[string]*gen.ScopeDefinition, len(result.Scopes))
	for _, scope := range result.Scopes {
		bySlug[scope.Slug] = scope
	}

	require.Equal(t, "org", bySlug[string(authz.ScopeOrgRead)].ResourceType)
	require.Equal(t, "project", bySlug[string(authz.ScopeProjectWrite)].ResourceType)
	require.Equal(t, "mcp", bySlug[string(authz.ScopeMCPConnect)].ResourceType)
	require.Equal(t, "environment", bySlug[string(authz.ScopeEnvironmentRead)].ResourceType)
	require.Equal(t, "environment", bySlug[string(authz.ScopeEnvironmentWrite)].ResourceType)
	require.Equal(t, "Read organization metadata and members.", bySlug[string(authz.ScopeOrgRead)].Description)
}

func TestService_ListScopes_Unauthorized(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = contextvalues.SetAuthContext(ctx, nil)

	_, err := ti.service.ListScopes(ctx, &gen.ListScopesPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing auth context")
}
