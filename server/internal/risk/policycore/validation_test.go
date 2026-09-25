package policycore

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestNormalizeAndValidateMCPScope(t *testing.T) {
	t.Parallel()

	serverID := uuid.New()
	scope, err := NormalizeMCPScope(&MCPScopeInput{
		ToolAnnotations: []string{" readOnlyHint ", "destructiveHint", "readOnlyHint"},
		Servers: []*MCPServerScopeInput{{
			MCPServerID: serverID.String(),
			Tools:       []string{" write ", "read", "read"},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"destructiveHint", "readOnlyHint"}, scope.ToolAnnotations)
	require.Equal(t, []MCPServerScope{{
		MCPServerID: serverID,
		Tools:       []string{"read", "write"},
	}}, scope.Servers)
	require.NoError(t, ValidateMCPScopeOwnership(scope, []uuid.UUID{serverID}))
	require.Error(t, ValidateMCPScopeOwnership(scope, nil))

	allServers, err := NormalizeMCPScope(&MCPScopeInput{
		AllServers:      true,
		ToolAnnotations: []string{"openWorldHint"},
	})
	require.NoError(t, err)
	require.True(t, allServers.AllServers)
	require.Empty(t, allServers.Servers)

	cleared, err := NormalizeMCPScope(&MCPScopeInput{Servers: []*MCPServerScopeInput{}})
	require.NoError(t, err)
	require.Nil(t, cleared)

	_, err = NormalizeMCPScope(&MCPScopeInput{ToolAnnotations: []string{"unknownHint"}})
	require.ErrorContains(t, err, "not recognized")

	_, err = NormalizeMCPScope(&MCPScopeInput{Servers: []*MCPServerScopeInput{{
		MCPServerID: serverID.String(),
		Tools:       []string{},
	}}})
	require.ErrorContains(t, err, "must include at least one tool")

	_, err = NormalizeMCPScope(&MCPScopeInput{Servers: []*MCPServerScopeInput{
		{MCPServerID: serverID.String()},
		{MCPServerID: serverID.String()},
	}})
	require.Error(t, err)
}

func TestValidateMCPScopeSources(t *testing.T) {
	t.Parallel()

	scope := &MCPScope{Servers: []MCPServerScope{{MCPServerID: uuid.New()}}}
	require.NoError(t, ValidateMCPScopeSources(nil, []string{ra.SourceAccountIdentity}))
	require.NoError(t, ValidateMCPScopeSources(scope, []string{ra.SourceGitleaks}))
	require.EqualError(
		t,
		ValidateMCPScopeSources(scope, []string{ra.SourceAccountIdentity}),
		`source "account_identity" cannot be used by an MCP-scoped policy`,
	)
	require.EqualError(
		t,
		ValidateMCPScopeSources(scope, []string{shadowmcp.SourceShadowMCP}),
		`source "shadow_mcp" cannot be used by an MCP-scoped policy`,
	)
}

func TestValidateActionAndSourceCompatibility(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"flag", "warn", "block", "quarantine"} {
		require.NoError(t, ValidateAction(action))
	}
	require.EqualError(t, ValidateAction("deny"), "action must be one of: flag, warn, block, quarantine")

	require.NoError(t, ValidateSources([]string{ra.SourceGitleaks, ra.SourcePresidio, shadowmcp.SourceShadowMCP}))
	require.EqualError(t, ValidateSources([]string{"unknown"}), `source "unknown" is not a recognized policy source`)

	flagOnlySources := []string{shadowmcp.SourceDestructiveTool, ra.SourceCLIDestructive, ra.SourceAccountIdentity}
	require.NoError(t, ValidateSourceAction(flagOnlySources, "flag"))
	for _, action := range []string{"warn", "block", "quarantine"} {
		for _, source := range flagOnlySources {
			require.Error(t, ValidateSourceAction([]string{source}, action))
		}
		require.NoError(t, ValidateSourceAction([]string{ra.SourceGitleaks}, action))
	}
}

func TestNormalizeApprovedEmailDomains(t *testing.T) {
	t.Parallel()

	got, err := NormalizeApprovedEmailDomains([]string{" Example.COM ", "@example.com", "", "sub.example.com"})
	require.NoError(t, err)
	require.Equal(t, []string{"example.com", "sub.example.com"}, got)

	_, err = NormalizeApprovedEmailDomains([]string{"not-a-domain"})
	require.EqualError(t, err, `approved email domain "not-a-domain" is not a valid domain`)
}

func TestValidatePolicyFields(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateName("Policy"))
	require.EqualError(t, ValidateName(""), "name must not be empty")
	require.EqualError(t, ValidateName(strings.Repeat("x", 101)), "name must be at most 100 characters")

	require.NoError(t, ValidatePolicyType(ra.PolicyTypeStandard))
	require.NoError(t, ValidatePolicyType(ra.PolicyTypePromptBased))
	require.EqualError(t, ValidatePolicyType("other"), "policy_type must be one of: standard, prompt_based")

	require.NoError(t, ValidateCustomRuleIDs([]string{"custom.rule_1"}))
	for _, id := range []string{"builtin.rule", "custom.", "custom.Bad", "custom.bad-rule"} {
		require.Error(t, ValidateCustomRuleIDs([]string{id}))
	}
}

func TestValidateDetectionScopes(t *testing.T) {
	t.Parallel()

	eng, err := celenv.New()
	require.NoError(t, err)
	include := ` kind == "user_message" `

	got, err := ValidateDetectionScopes(eng, []*DetectionScopeInput{{
		Category:     "prompt_injection",
		ScopeInclude: &include,
	}})
	require.NoError(t, err)
	require.Equal(t, []ra.DetectionScopeConfig{{
		Category:     "prompt_injection",
		ScopeInclude: `kind == "user_message"`,
		ScopeExempt:  "",
	}}, got)

	invalidCEL := "kind ="
	_, err = ValidateDetectionScopes(eng, []*DetectionScopeInput{{
		Category:     "secrets",
		ScopeInclude: &invalidCEL,
	}})
	var validationErr *ValidationError
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, `detection scope for "secrets" does not compile`, validationErr.Message)
	require.Error(t, validationErr.Cause)

	invalid := []struct {
		name   string
		scopes []*DetectionScopeInput
	}{
		{name: "null", scopes: []*DetectionScopeInput{nil}},
		{name: "unknown", scopes: []*DetectionScopeInput{{Category: "unknown"}}},
		{name: "session scoped", scopes: []*DetectionScopeInput{{Category: "account_identity"}}},
		{name: "duplicate", scopes: []*DetectionScopeInput{{Category: "secrets"}, {Category: "secrets"}}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateDetectionScopes(eng, test.scopes)
			require.Error(t, err)
		})
	}
}

// The registry answers "is there a recommended scope", not "may this category
// carry one". `custom` has no recommendation, but the scanner honours a
// specified scope for it and the legacy-policy-scope fold writes one.
func TestValidateDetectionScopesAcceptsCategoryWithoutRecommendation(t *testing.T) {
	t.Parallel()

	eng, err := celenv.New()
	require.NoError(t, err)

	_, ok := recommendedscopes.For(categories.CategoryCustom)
	require.False(t, ok, "custom is deliberately absent from the registry")

	include := `kind in ["tool_request"]`
	got, err := ValidateDetectionScopes(eng, []*DetectionScopeInput{{
		Category: string(categories.CategoryCustom), ScopeInclude: &include, ScopeExempt: nil,
	}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "custom", got[0].Category)
	require.Equal(t, include, got[0].ScopeInclude)
}
