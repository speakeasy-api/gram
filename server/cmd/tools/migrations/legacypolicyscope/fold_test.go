package legacypolicyscope_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/legacypolicyscope"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
)

const assistantExempt = `kind == "assistant_message"`

func testCatalog(t *testing.T) policycatalog.Catalog {
	t.Helper()
	catalog, err := policycatalog.Build()
	require.NoError(t, err)
	return catalog
}

func scopeFor(t *testing.T, scopes []ra.DetectionScopeConfig, category string) ra.DetectionScopeConfig {
	t.Helper()
	for _, scope := range scopes {
		if scope.Category == category {
			return scope
		}
	}
	t.Fatalf("no detection scope for category %q in %v", category, scopes)
	return ra.DetectionScopeConfig{Category: "", ScopeInclude: "", ScopeExempt: ""}
}

func TestFoldNoLegacyScopeIsNoop(t *testing.T) {
	t.Parallel()

	existing := []ra.DetectionScopeConfig{{Category: "secrets", ScopeInclude: `kind == "user_message"`, ScopeExempt: ""}}
	got, err := legacypolicyscope.Fold(legacypolicyscope.Policy{
		Action: "block", PolicyType: "standard", Sources: []string{"gitleaks"},
		CustomRuleIDs: nil, MessageTypes: nil, ScopeInclude: "", ScopeExempt: "",
		DetectionScopes: existing,
	}, testCatalog(t))

	require.NoError(t, err)
	require.Equal(t, legacypolicyscope.DispositionNoop, got.Disposition)
	require.Equal(t, existing, got.DetectionScopes)
}

func TestFoldFlagPolicyClearsLegacyScope(t *testing.T) {
	t.Parallel()

	existing := []ra.DetectionScopeConfig{{Category: "secrets", ScopeInclude: `kind == "user_message"`, ScopeExempt: ""}}
	got, err := legacypolicyscope.Fold(legacypolicyscope.Policy{
		Action: "flag", PolicyType: "standard", Sources: []string{"gitleaks"},
		CustomRuleIDs: nil, MessageTypes: []string{"tool_request", "tool_response"},
		ScopeInclude: "", ScopeExempt: "", DetectionScopes: existing,
	}, testCatalog(t))

	require.NoError(t, err)
	require.Equal(t, legacypolicyscope.DispositionCleared, got.Disposition)
	// The legacy narrowing is dropped; the category's own scope is untouched.
	require.Equal(t, existing, got.DetectionScopes)
}

func TestFoldEnforcingPolicyComposesLegacyWithRecommendation(t *testing.T) {
	t.Parallel()

	got, err := legacypolicyscope.Fold(legacypolicyscope.Policy{
		Action: "block", PolicyType: "standard", Sources: []string{"gitleaks"},
		CustomRuleIDs: nil, MessageTypes: []string{"tool_response", "tool_request"},
		ScopeInclude: "", ScopeExempt: "", DetectionScopes: nil,
	}, testCatalog(t))

	require.NoError(t, err)
	require.Equal(t, legacypolicyscope.DispositionPreserved, got.Disposition)

	secrets := scopeFor(t, got.DetectionScopes, "secrets")
	// Message types are sorted and encoded exactly as the editor round-trips them.
	require.Equal(t, `kind in ["tool_request","tool_response"]`, secrets.ScopeInclude)
	// The recommendation survives the fold rather than being replaced by it.
	require.Equal(t, assistantExempt, secrets.ScopeExempt)
}

func TestFoldWarnCountsAsEnforcing(t *testing.T) {
	t.Parallel()

	got, err := legacypolicyscope.Fold(legacypolicyscope.Policy{
		Action: "warn", PolicyType: "standard", Sources: []string{"gitleaks"},
		CustomRuleIDs: nil, MessageTypes: []string{"tool_request"},
		ScopeInclude: "", ScopeExempt: "", DetectionScopes: nil,
	}, testCatalog(t))

	require.NoError(t, err)
	require.Equal(t, legacypolicyscope.DispositionPreserved, got.Disposition)
}

func TestFoldPrefersSpecifiedScopeOverRecommendation(t *testing.T) {
	t.Parallel()

	got, err := legacypolicyscope.Fold(legacypolicyscope.Policy{
		Action: "block", PolicyType: "standard", Sources: []string{"gitleaks"},
		CustomRuleIDs: nil, MessageTypes: []string{"tool_request"},
		ScopeInclude: "", ScopeExempt: "",
		DetectionScopes: []ra.DetectionScopeConfig{
			{Category: "secrets", ScopeInclude: `tool_calls.exists(t, t.name.matchRegex("Bash"))`, ScopeExempt: ""},
		},
	}, testCatalog(t))

	require.NoError(t, err)
	secrets := scopeFor(t, got.DetectionScopes, "secrets")
	require.Equal(t, `(kind in ["tool_request"]) && (tool_calls.exists(t, t.name.matchRegex("Bash")))`, secrets.ScopeInclude)
	// The category opted out of the recommendation, so the fold must not
	// reintroduce the registry's exemption.
	require.Empty(t, secrets.ScopeExempt)
}

func TestFoldComposesLegacyIncludeAndExempt(t *testing.T) {
	t.Parallel()

	got, err := legacypolicyscope.Fold(legacypolicyscope.Policy{
		Action: "block", PolicyType: "standard", Sources: []string{"gitleaks"},
		CustomRuleIDs: nil, MessageTypes: []string{"tool_request"},
		ScopeInclude:    `tool_calls.exists(t, t.name.matchRegex("Bash"))`,
		ScopeExempt:     `kind == "user_message"`,
		DetectionScopes: nil,
	}, testCatalog(t))

	require.NoError(t, err)
	secrets := scopeFor(t, got.DetectionScopes, "secrets")
	require.Equal(t, `(kind in ["tool_request"]) && (tool_calls.exists(t, t.name.matchRegex("Bash")))`, secrets.ScopeInclude)
	require.Equal(t, `(kind == "user_message") || (`+assistantExempt+`)`, secrets.ScopeExempt)
}

func TestFoldSkipsSessionScopedCategory(t *testing.T) {
	t.Parallel()

	got, err := legacypolicyscope.Fold(legacypolicyscope.Policy{
		Action: "block", PolicyType: "standard",
		Sources:       []string{"gitleaks", "account_identity"},
		CustomRuleIDs: nil, MessageTypes: []string{"tool_request"},
		ScopeInclude: "", ScopeExempt: "", DetectionScopes: nil,
	}, testCatalog(t))

	require.NoError(t, err)
	for _, scope := range got.DetectionScopes {
		require.NotEqual(t, "account_identity", scope.Category,
			"message scoping does not apply to a session-scoped category")
	}
}

func TestFoldCoversCustomRulesAndPromptPolicies(t *testing.T) {
	t.Parallel()

	custom, err := legacypolicyscope.Fold(legacypolicyscope.Policy{
		Action: "block", PolicyType: "standard", Sources: nil,
		CustomRuleIDs: []string{"rule-1"}, MessageTypes: []string{"tool_request"},
		ScopeInclude: "", ScopeExempt: "", DetectionScopes: nil,
	}, testCatalog(t))
	require.NoError(t, err)
	require.Equal(t, `kind in ["tool_request"]`, scopeFor(t, custom.DetectionScopes, "custom").ScopeInclude)

	prompt, err := legacypolicyscope.Fold(legacypolicyscope.Policy{
		Action: "block", PolicyType: "prompt_based", Sources: nil,
		CustomRuleIDs: nil, MessageTypes: []string{"tool_request"},
		ScopeInclude: "", ScopeExempt: "", DetectionScopes: nil,
	}, testCatalog(t))
	require.NoError(t, err)
	require.Equal(t, `kind in ["tool_request"]`, scopeFor(t, prompt.DetectionScopes, "prompt_policy").ScopeInclude)
}

func TestFoldRefusesEnforcingPolicyWithNoCategories(t *testing.T) {
	t.Parallel()

	_, err := legacypolicyscope.Fold(legacypolicyscope.Policy{
		Action: "block", PolicyType: "standard", Sources: []string{"not_a_source"},
		CustomRuleIDs: nil, MessageTypes: []string{"tool_request"},
		ScopeInclude: "", ScopeExempt: "", DetectionScopes: nil,
	}, testCatalog(t))

	require.ErrorIs(t, err, legacypolicyscope.ErrNoCategories)
}

func TestFoldIsIdempotentAndDeterministic(t *testing.T) {
	t.Parallel()

	policy := legacypolicyscope.Policy{
		Action: "block", PolicyType: "standard",
		Sources:       []string{"presidio", "gitleaks"},
		CustomRuleIDs: nil, MessageTypes: []string{"tool_response", "tool_request"},
		ScopeInclude: "", ScopeExempt: "", DetectionScopes: nil,
	}

	first, err := legacypolicyscope.Fold(policy, testCatalog(t))
	require.NoError(t, err)
	again, err := legacypolicyscope.Fold(policy, testCatalog(t))
	require.NoError(t, err)
	require.Equal(t, first.DetectionScopes, again.DetectionScopes)

	// The legacy columns are cleared by the runner, so re-reading the folded row
	// is a noop rather than a second composition.
	folded := policy
	folded.MessageTypes = nil
	folded.DetectionScopes = first.DetectionScopes
	third, err := legacypolicyscope.Fold(folded, testCatalog(t))
	require.NoError(t, err)
	require.Equal(t, legacypolicyscope.DispositionNoop, third.Disposition)
	require.Equal(t, first.DetectionScopes, third.DetectionScopes)
}

// Every expression the fold emits has to survive the real engine, otherwise the
// scanner fails closed on a policy that used to work.
func TestFoldEmitsCompilableCEL(t *testing.T) {
	t.Parallel()

	eng, err := celenv.New()
	require.NoError(t, err)

	for _, source := range []string{"gitleaks", "presidio", "prompt_injection", "shadow_mcp", "cli_destructive"} {
		got, err := legacypolicyscope.Fold(legacypolicyscope.Policy{
			Action: "block", PolicyType: "standard", Sources: []string{source},
			CustomRuleIDs: nil, MessageTypes: []string{"tool_request", "tool_response"},
			ScopeInclude:    `tool_calls.exists(t, t.name.matchRegex("Bash"))`,
			ScopeExempt:     `kind == "user_message"`,
			DetectionScopes: nil,
		}, testCatalog(t))
		require.NoError(t, err, source)

		for _, scope := range got.DetectionScopes {
			_, err := ra.CompileScope(eng, scope.ScopeInclude, scope.ScopeExempt)
			require.NoError(t, err, "%s/%s", source, scope.Category)
		}
	}
}
