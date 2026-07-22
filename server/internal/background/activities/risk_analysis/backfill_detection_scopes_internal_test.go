package risk_analysis

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestMessageTypesExpr(t *testing.T) {
	t.Parallel()

	require.Empty(t, messageTypesExpr(nil))
	require.Equal(t, `kind == "user_message"`, messageTypesExpr([]string{message.User}))
	require.Equal(t,
		`kind == "tool_request" || kind == "tool_response"`,
		messageTypesExpr([]string{message.ToolRequest, message.ToolResponse}),
	)
}

func TestScopeExprComposition(t *testing.T) {
	t.Parallel()

	require.Empty(t, IntersectScopeExprs("", ""))
	require.Equal(t, "a", IntersectScopeExprs("a", ""))
	require.Equal(t, "b", IntersectScopeExprs("", "b"))
	require.Equal(t, "(a) && (b)", IntersectScopeExprs("a", "b"))

	require.Empty(t, UnionScopeExprs("", ""))
	require.Equal(t, "a", UnionScopeExprs("a", ""))
	require.Equal(t, "(a) || (b)", UnionScopeExprs("a", "b"))
}

func mustEng(t *testing.T) *celenv.Engine {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	return eng
}

func TestComposeLegacyScopeMessageTypesIntoSourceCategories(t *testing.T) {
	t.Parallel()

	config, err := composeLegacyScope(mustEng(t), repo.RiskPolicy{
		Sources:      []string{SourceGitleaks},
		MessageTypes: []string{message.ToolRequest, message.ToolResponse},
	})
	require.NoError(t, err)

	specs := DetectionScopesFromConfig(config)
	require.Len(t, specs, 1)
	require.Equal(t, "secrets", specs[0].Category)
	require.Equal(t, `kind == "tool_request" || kind == "tool_response"`, specs[0].ScopeInclude)
	require.Equal(t, `kind == "assistant_message"`, specs[0].ScopeExempt, "registry recommendation stays the exempt base")
}

func TestComposeLegacyScopeUnionsExemptOntoSpecified(t *testing.T) {
	t.Parallel()

	base, err := WithDetectionScopes(nil, []DetectionScopeConfig{
		{Category: "secrets", ScopeInclude: `kind == "user_message"`, ScopeExempt: ""},
	})
	require.NoError(t, err)

	config, err := composeLegacyScope(mustEng(t), repo.RiskPolicy{
		Sources:        []string{SourceGitleaks},
		ScopeInclude:   pgtype.Text{String: `content.matchText("secret")`, Valid: true},
		ScopeExempt:    pgtype.Text{String: `content.matchText("test")`, Valid: true},
		AnalyzerConfig: base,
	})
	require.NoError(t, err)

	specs := DetectionScopesFromConfig(config)
	require.Len(t, specs, 1)
	require.Equal(t, "secrets", specs[0].Category)
	require.Equal(t, `(kind == "user_message") && (content.matchText("secret"))`, specs[0].ScopeInclude)
	require.Equal(t, `content.matchText("test")`, specs[0].ScopeExempt)
}

func TestComposeLegacyScopeCoversCustomAndPromptPolicy(t *testing.T) {
	t.Parallel()

	config, err := composeLegacyScope(mustEng(t), repo.RiskPolicy{
		PolicyType:    PolicyTypePromptBased,
		Sources:       []string{},
		CustomRuleIds: []string{"custom.acme"},
		MessageTypes:  []string{message.User},
	})
	require.NoError(t, err)

	byCat := map[string]DetectionScopeConfig{}
	for _, spec := range DetectionScopesFromConfig(config) {
		byCat[spec.Category] = spec
	}
	require.Len(t, byCat, 2)
	require.Equal(t, `kind == "user_message"`, byCat["custom"].ScopeInclude)
	require.Empty(t, byCat["custom"].ScopeExempt, "custom has no registry exempt to inherit")
	require.Equal(t, `kind == "user_message"`, byCat["prompt_policy"].ScopeInclude)
	require.Equal(t, `kind == "assistant_message"`, byCat["prompt_policy"].ScopeExempt)
}

func TestComposeLegacyScopeSkipsSessionScopedCategories(t *testing.T) {
	t.Parallel()

	config, err := composeLegacyScope(mustEng(t), repo.RiskPolicy{
		Sources:      []string{SourceAccountIdentity},
		MessageTypes: []string{message.ToolRequest},
	})
	require.NoError(t, err)
	require.Empty(t, DetectionScopesFromConfig(config))
}
