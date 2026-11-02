package access

import (
	"os"
	"path/filepath"
	"regexp"
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
	require.Len(t, result.Scopes, len(authz.RegisteredScopes())-1, "every registered scope but root is listed")

	bySlug := make(map[string]*gen.ScopeDefinition, len(result.Scopes))
	for _, scope := range result.Scopes {
		bySlug[scope.Slug] = scope
	}
	require.NotContains(t, bySlug, string(authz.ScopeRoot))

	// Presentation metadata comes from the authz catalogue, not from this
	// package: a label, a grouping, the included scopes, and whether granting
	// the scope deserves a warning.
	require.Equal(t, "View tool logs", bySlug[string(authz.ScopeLogRead)].Label)
	require.Equal(t, "log", bySlug[string(authz.ScopeLogRead)].Category)
	require.True(t, bySlug[string(authz.ScopeLogRead)].Sensitive)
	require.Equal(t, "tool_logs", *bySlug[string(authz.ScopeLogRead)].Sensitivity)
	require.Empty(t, bySlug[string(authz.ScopeLogRead)].SubScopes)
	require.Equal(t, "Publish plugins", bySlug[string(authz.ScopePluginWrite)].Label)
	require.Equal(t, "plugin", bySlug[string(authz.ScopePluginWrite)].Category)
	require.Equal(t, "Transfer agent ownership", bySlug[string(authz.ScopeAgentTransfer)].Label)
	require.False(t, bySlug[string(authz.ScopeAgentTransfer)].Sensitive)
	require.Nil(t, bySlug[string(authz.ScopeAgentTransfer)].Sensitivity)
	require.Equal(t, "session", bySlug[string(authz.ScopeChatRead)].Category)
	require.Equal(t, "conversations", *bySlug[string(authz.ScopeChatRead)].Sensitivity)
	require.Equal(t, []string{"chat:read"}, bySlug[string(authz.ScopeChatWrite)].SubScopes)
	require.True(t, bySlug[string(authz.ScopeChatWrite)].Sensitive)
	require.Equal(t, "destructive_admin", *bySlug[string(authz.ScopeOrgAdmin)].Sensitivity)
	require.Equal(t, []string{"mcp:connect", "mcp:read"}, bySlug[string(authz.ScopeMCPWrite)].SubScopes)
	require.False(t, bySlug[string(authz.ScopeEnvironmentRead)].Sensitive)
	for _, scope := range result.Scopes {
		require.NotEmpty(t, scope.Label, scope.Slug)
		require.NotEmpty(t, scope.Category, scope.Slug)
		require.NotNil(t, scope.SubScopes, "%s: sub_scopes is a list, never null", scope.Slug)
		require.Equal(t, scope.Sensitive, scope.Sensitivity != nil, scope.Slug)
	}

	// Tasks come from the same package and only name catalogued scopes.
	require.Len(t, result.Tasks, len(authz.Tasks()))
	for _, task := range result.Tasks {
		require.NotEmpty(t, task.Slug)
		require.NotEmpty(t, task.Label)
		for _, scope := range task.Scopes {
			require.Contains(t, bySlug, scope, "%s: %s is not a listed scope", task.Slug, scope)
		}
	}
	require.Equal(t, "session_review", result.Tasks[2].Slug)
	require.Equal(t, []string{"chat:read", "log:read"}, result.Tasks[2].Scopes)

	require.Equal(t, "org", bySlug[string(authz.ScopeOrgRead)].ResourceType)
	require.Equal(t, "project", bySlug[string(authz.ScopeProjectWrite)].ResourceType)
	require.Equal(t, "mcp", bySlug[string(authz.ScopeMCPConnect)].ResourceType)
	require.Equal(t, "environment", bySlug[string(authz.ScopeEnvironmentRead)].ResourceType)
	require.Equal(t, "environment", bySlug[string(authz.ScopeEnvironmentWrite)].ResourceType)
	require.Equal(t, "skill", bySlug[string(authz.ScopeSkillRead)].ResourceType)
	require.Equal(t, "skill", bySlug[string(authz.ScopeSkillWrite)].ResourceType)
	require.Equal(t, "project", bySlug[string(authz.ScopePluginWrite)].ResourceType)
	require.Equal(t, authz.ScopeVisibilityUserVisible, bySlug[string(authz.ScopePluginWrite)].Visibility)
	require.Equal(t, string(authz.ScopePluginBlockedWrite), *bySlug[string(authz.ScopePluginWrite)].ExclusionScope)
	require.Equal(t, authz.ScopeVisibilityInternal, bySlug[string(authz.ScopePluginBlockedWrite)].Visibility)
	require.Equal(t, "assistant", bySlug[string(authz.ScopeAssistantRead)].ResourceType)
	require.Equal(t, "assistant", bySlug[string(authz.ScopeAssistantWrite)].ResourceType)
	require.Equal(t, "risk_policy", bySlug[string(authz.ScopeRiskPolicyEvaluate)].ResourceType)
	require.Equal(t, "risk_policy", bySlug[string(authz.ScopeRiskPolicyBypass)].ResourceType)
	require.Equal(t, "chat", bySlug[string(authz.ScopeChatRead)].ResourceType)
	require.Equal(t, "inference", bySlug[string(authz.ScopeInferenceExecute)].ResourceType)
	require.Equal(t, authz.ScopeVisibilityUserVisible, bySlug[string(authz.ScopeInferenceExecute)].Visibility)
	require.True(t, bySlug[string(authz.ScopeInferenceExecute)].AgentEligible)
	require.Nil(t, bySlug[string(authz.ScopeInferenceExecute)].ExclusionScope)
	require.Equal(t, "project", bySlug[string(authz.ScopeLogRead)].ResourceType)
	require.Equal(t, authz.ScopeVisibilityUserVisible, bySlug[string(authz.ScopeLogRead)].Visibility)
	require.Nil(t, bySlug[string(authz.ScopeLogRead)].ExclusionScope)
	for _, scope := range []authz.Scope{authz.ScopeMemoryRead, authz.ScopeMemoryWrite} {
		require.Equal(t, "project", bySlug[string(scope)].ResourceType)
		require.Equal(t, authz.ScopeVisibilityUserVisible, bySlug[string(scope)].Visibility)
		require.Nil(t, bySlug[string(scope)].ExclusionScope)
	}
	for _, scope := range []authz.Scope{authz.ScopeAgentRead, authz.ScopeAgentWrite, authz.ScopeAgentAuthorize, authz.ScopeAgentTransfer} {
		require.Equal(t, "agent", bySlug[string(scope)].ResourceType)
		require.Equal(t, authz.ScopeVisibilityUserVisible, bySlug[string(scope)].Visibility)
		require.Nil(t, bySlug[string(scope)].ExclusionScope)
	}
	// Mirrors the families above: the count alone would still pass if these were
	// mapped to another resource type or hidden as internal.
	for _, scope := range []authz.Scope{authz.ScopeWorkloadRead, authz.ScopeWorkloadWrite} {
		require.Equal(t, "workload", bySlug[string(scope)].ResourceType)
		require.Equal(t, authz.ScopeVisibilityUserVisible, bySlug[string(scope)].Visibility)
	}
	require.Equal(t, string(authz.ScopeWorkloadBlockedRead), *bySlug[string(authz.ScopeWorkloadRead)].ExclusionScope)
	require.Equal(t, string(authz.ScopeWorkloadBlockedWrite), *bySlug[string(authz.ScopeWorkloadWrite)].ExclusionScope)
	require.Equal(t, "View organization settings and members.", bySlug[string(authz.ScopeOrgRead)].Description)
	require.Equal(t, authz.ScopeVisibilityUserVisible, bySlug[string(authz.ScopeProjectWrite)].Visibility)
	require.Equal(t, authz.ScopeVisibilityInternal, bySlug[string(authz.ScopeProjectBlockedWrite)].Visibility)
	require.NotNil(t, bySlug[string(authz.ScopeProjectWrite)].ExclusionScope)
	require.Equal(t, string(authz.ScopeProjectBlockedWrite), *bySlug[string(authz.ScopeProjectWrite)].ExclusionScope)
	require.Nil(t, bySlug[string(authz.ScopeProjectBlockedWrite)].ExclusionScope)
	require.NotNil(t, bySlug[string(authz.ScopeMCPConnect)].ExclusionScope)
	require.Equal(t, string(authz.ScopeMCPBlockedConnect), *bySlug[string(authz.ScopeMCPConnect)].ExclusionScope)
	require.Nil(t, bySlug[string(authz.ScopeMCPBlockedConnect)].ExclusionScope)
	require.Equal(t, authz.ScopeVisibilityUserVisible, bySlug[string(authz.ScopeSkillWrite)].Visibility)
	require.Equal(t, authz.ScopeVisibilityInternal, bySlug[string(authz.ScopeSkillBlockedWrite)].Visibility)
	require.NotNil(t, bySlug[string(authz.ScopeSkillWrite)].ExclusionScope)
	require.Equal(t, string(authz.ScopeSkillBlockedWrite), *bySlug[string(authz.ScopeSkillWrite)].ExclusionScope)
	require.Equal(t, authz.ScopeVisibilityUserVisible, bySlug[string(authz.ScopeAssistantWrite)].Visibility)
	require.Equal(t, authz.ScopeVisibilityInternal, bySlug[string(authz.ScopeAssistantBlockedWrite)].Visibility)
	require.NotNil(t, bySlug[string(authz.ScopeAssistantWrite)].ExclusionScope)
	require.Equal(t, string(authz.ScopeAssistantBlockedWrite), *bySlug[string(authz.ScopeAssistantWrite)].ExclusionScope)
	require.NotNil(t, bySlug[string(authz.ScopeRiskPolicyEvaluate)].ExclusionScope)
	require.Equal(t, string(authz.ScopeRiskPolicyBypass), *bySlug[string(authz.ScopeRiskPolicyEvaluate)].ExclusionScope)
	require.Nil(t, bySlug[string(authz.ScopeRiskPolicyBypass)].ExclusionScope)

	// Agent eligibility mirrors the agent runtime scope registry: a role may
	// carry scopes its agent members cannot hold, and the editor says so.
	for _, scope := range []authz.Scope{authz.ScopeMCPConnect, authz.ScopeMCPRead, authz.ScopeMCPWrite, authz.ScopeProjectRead, authz.ScopeSkillWrite, authz.ScopeRiskPolicyEvaluate, authz.ScopeMCPBlockedConnect} {
		require.True(t, bySlug[string(scope)].AgentEligible, scope)
	}
	for _, scope := range []authz.Scope{authz.ScopeOrgAdmin, authz.ScopeOrgRead, authz.ScopeChatRead, authz.ScopeAgentWrite, authz.ScopeRiskPolicyBypass, authz.ScopePluginBlockedWrite} {
		require.False(t, bySlug[string(scope)].AgentEligible, scope)
	}
}

// The scope enums come from the authz catalogue, but a category or sensitivity
// the catalogue emits and the design does not declare still makes generated
// clients reject the whole listScopes response.
func TestService_ListScopes_CategoriesAndSensitivitiesAreInTheDesignEnum(t *testing.T) {
	t.Parallel()

	design, err := os.ReadFile(filepath.Join("..", "..", "design", "access", "design.go"))
	require.NoError(t, err)
	// Other types declare a "category" too; only ScopeDefinition's counts.
	scopeModel := regexp.MustCompile(`(?s)Type\("ScopeDefinition".*?\n\}\)`).Find(design)
	require.NotNil(t, scopeModel, "could not find the ScopeDefinition type in the access design")

	declaredEnum := func(attribute string) map[string]bool {
		block := regexp.MustCompile(`(?s)Attribute\("` + attribute + `".*?Enum\((.*?)\)`).FindSubmatch(scopeModel)
		require.NotNil(t, block, "could not find the %s enum in the access design", attribute)
		declared := make(map[string]bool)
		for _, quoted := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(block[1], -1) {
			declared[string(quoted[1])] = true
		}
		require.NotEmpty(t, declared)
		return declared
	}
	categories := declaredEnum("category")
	sensitivities := declaredEnum("sensitivity")

	for _, category := range authz.ScopeCategories {
		require.True(t, categories[string(category)], "category %q is not declared in the design", category)
	}

	ctx, ti := newTestAccessService(t)
	result, err := ti.service.ListScopes(ctx, &gen.ListScopesPayload{})
	require.NoError(t, err)

	for _, scope := range result.Scopes {
		require.True(t, categories[scope.Category], "scope %q has category %q, which the design does not declare", scope.Slug, scope.Category)
		if scope.Sensitivity != nil {
			require.True(t, sensitivities[*scope.Sensitivity], "scope %q has sensitivity %q, which the design does not declare", scope.Slug, *scope.Sensitivity)
		}
	}
}

func TestService_ListScopes_Unauthorized(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = contextvalues.SetAuthContext(ctx, nil)

	_, err := ti.service.ListScopes(ctx, &gen.ListScopesPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing auth context")
}
