package risk_analysis

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/accountidentity"
	"github.com/speakeasy-api/gram/server/internal/scanners/destructivetool"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func mustRecommendedSet(t *testing.T) RecommendedSet {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	set, err := CompileRecommended(eng)
	require.NoError(t, err)
	return set
}

func msg(typ message.Type) batchMessage {
	return batchMessage{
		ID:                  uuid.New(),
		ChatID:              uuid.Nil,
		ParentChatMessageID: uuid.Nil,
		ContentPart:         false,
		Type:                typ,
		Content:             "content",
		RawToolCalls:        nil,
		ToolCalls:           []recordedToolCall{},
		UserID:              "",
		CreatedAt:           time.Time{},
		Source:              "",
	}
}

func toolReq(names ...string) batchMessage {
	out := msg(message.ToolRequest)
	out.Content = ""
	out.ToolCalls = make([]recordedToolCall, 0, len(names))
	for _, name := range names {
		var call recordedToolCall
		call.Function.Name = name
		call.Function.Arguments = `{"command":"rm -rf /tmp/data"}`
		out.ToolCalls = append(out.ToolCalls, call)
	}
	return out
}

func finding(source, ruleID string) scanners.Finding {
	return scanners.Finding{
		RuleID:              ruleID,
		Description:         "",
		Match:               "",
		StartPos:            0,
		EndPos:              0,
		Tags:                []string{},
		Source:              source,
		Confidence:          1,
		DeadLetterReason:    "",
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	}
}

func masksFor(t *testing.T, specified []DetectionScopeConfig, messages []batchMessage) CategoryScopeMasks {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	compiled, err := CompileDetectionScopes(eng, specified)
	require.NoError(t, err)
	return NewCategoryScopes(mustRecommendedSet(t), compiled).Masks(t.Context(), messages)
}

func TestRecommendedCategoryScopesPromptInjectionBehavior(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{
		msg(message.Assistant),
		msg(message.ToolResponse),
		msg(message.User),
	}
	masks := masksFor(t, nil, messages)
	require.False(t, masks.InScope(0, categories.CategoryPromptInjection), "assistant_message is out of the prompt_injection recommendation")
	require.True(t, masks.InScope(1, categories.CategoryPromptInjection))
	require.True(t, masks.InScope(2, categories.CategoryPromptInjection))
}

func TestRecommendedCategoryScopesMultiSourceSameMessage(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{msg(message.Assistant), msg(message.ToolResponse)}
	masks := masksFor(t, nil, messages)
	require.False(t, masks.InScope(0, categories.CategoryPromptInjection), "assistant messages are out of scope for every category")
	require.False(t, masks.InScope(0, categories.CategorySecrets))
	require.True(t, masks.InScope(1, categories.CategoryPromptInjection))
	require.True(t, masks.InScope(1, categories.CategorySecrets))
}

func TestDetectionScopeUnrestrictedKeepsPromptInjection(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{msg(message.Assistant)}
	masks := masksFor(t, []DetectionScopeConfig{
		{Category: string(categories.CategoryPromptInjection), ScopeInclude: "", ScopeExempt: ""},
	}, messages)

	require.True(t, masks.InScope(0, categories.CategoryPromptInjection))
}

func TestDetectionScopeSpecifiedWinsOverRecommendation(t *testing.T) {
	t.Parallel()

	// Recommendation admits tool responses for prompt injection; the specified
	// scope narrows to user messages only and must win for its category while
	// leaving other categories on their recommendations.
	messages := []batchMessage{
		msg(message.User),
		msg(message.ToolResponse),
		toolReq("Bash"),
	}
	masks := masksFor(t, []DetectionScopeConfig{
		{Category: string(categories.CategoryPromptInjection), ScopeInclude: `kind == "user_message"`, ScopeExempt: ""},
	}, messages)

	require.True(t, masks.InScope(0, categories.CategoryPromptInjection))
	require.False(t, masks.InScope(1, categories.CategoryPromptInjection))
	require.True(t, masks.InScope(2, categories.CategoryCLIDestructive))
}

func TestRecommendedCategoryScopesToolRequestOnlyCategories(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{
		toolReq("Bash"),
		msg(message.Assistant),
	}
	masks := masksFor(t, nil, messages)
	require.True(t, masks.InScope(0, categories.CategoryCLIDestructive))
	require.False(t, masks.InScope(1, categories.CategoryCLIDestructive))
}

func TestRecommendedCategoryScopesSubset(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{
		msg(message.Assistant),
		msg(message.ToolResponse),
		toolReq("Read"),
		toolReq("Bash"),
	}
	masks := masksFor(t, nil, messages)
	require.Equal(t, 2, masks.RecommendedPrefilteredCount(sourceCategories[SourcePromptInjection]))
	require.Equal(t, 1, masks.RecommendedPrefilteredCount(sourceCategories[SourcePresidio]))

	require.Equal(t, []batchMessage{messages[1], messages[3]}, masks.Admitted(messages, sourceCategories[SourcePromptInjection]))
	require.Equal(t, messages[1:], masks.Admitted(messages, sourceCategories[SourcePresidio]))
}

func TestSubsetWithoutCategoryMasksScansEverything(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{msg(message.Assistant), msg(message.User)}
	// No recommendation and no specified scope, so there is no mask at all.
	masks := NewCategoryScopes(RecommendedSet{scopes: nil}, nil).Masks(t.Context(), messages)

	require.Equal(t, messages, masks.Admitted(messages, sourceCategories[SourcePresidio]), "no category mask for presidio: every message is published")
	require.Equal(t, 0, masks.RecommendedPrefilteredCount(sourceCategories[SourcePresidio]))
}

func TestSourceCategoriesConsistentWithClassify(t *testing.T) {
	t.Parallel()

	// One representative finding per (category, emitting source). The LLM
	// analyzer stands in for several legacy sources, so the categories it
	// covers list a second representative carrying its source and the rule
	// id it emits for that source's policies.
	representative := map[categories.Category][]scanners.Finding{
		categories.CategoryAccountIdentity: {finding(accountidentity.Source, "identity.unapproved_domain")},
		categories.CategorySecrets:         {finding(SourceGitleaks, "secret.generic-api-key"), finding(llmanalyzer.Source, llmanalyzer.RuleSecret)},
		categories.CategoryFinancial:       {finding(SourcePresidio, "pii.credit_card")},
		categories.CategoryGovernmentIDs:   {finding(SourcePresidio, "pii.us_ssn")},
		categories.CategoryHealthcare:      {finding(SourcePresidio, "pii.us_mbi")},
		categories.CategoryOffPolicy:       {finding(SourcePresidio, "pii.policy_violation")},
		categories.CategoryPII:             {finding(SourcePresidio, "pii.email_address"), finding(llmanalyzer.Source, llmanalyzer.RulePII)},
		categories.CategoryPromptInjection: {finding(SourcePromptInjection, promptinjection.Rule), finding(llmanalyzer.Source, llmanalyzer.RulePromptInjection)},
		categories.CategoryShadowMCP:       {finding(shadowmcp.SourceShadowMCP, "shadow_mcp")},
		categories.CategoryDestructiveTool: {finding(shadowmcp.SourceDestructiveTool, destructivetool.Rule), finding(llmanalyzer.Source, llmanalyzer.RuleDestructiveTool)},
		categories.CategoryCLIDestructive:  {finding(SourceCLIDestructive, "cli_destructive.rm_rf"), finding(llmanalyzer.Source, llmanalyzer.RuleCLIDestructive)},
		categories.CategoryPromptPolicy:    {finding(promptpolicy.Source, promptpolicy.Rule)},
		categories.CategoryCustom:          {finding(SourceCustom, "custom.test")},
	}

	reachable := make(map[categories.Category]bool)
	for source, cats := range sourceCategories {
		require.NotEmpty(t, cats)
		for _, cat := range cats {
			reps, ok := representative[cat]
			require.Truef(t, ok, "missing representative for %s", cat)
			idx := slices.IndexFunc(reps, func(f scanners.Finding) bool { return f.Source == source })
			require.NotEqualf(t, -1, idx, "missing representative for %s from source %s", cat, source)
			rep := reps[idx]
			got := categories.Classify(rep.Source, rep.RuleID)
			require.Truef(t, sourceCanEmit(source, got), "%s classified as %s, outside source map", source, got)
			reachable[got] = true
		}
	}

	for _, def := range categories.All() {
		if def.Category == categories.CategoryCustom || def.Category == categories.CategoryAccountIdentity {
			continue
		}
		require.Truef(t, reachable[def.Category], "category %s is not reachable from sourceCategories", def.Category)
	}
}

func TestDetectionScopesConfig(t *testing.T) {
	t.Parallel()

	require.Nil(t, DetectionScopesFromConfig(nil))
	require.Nil(t, DetectionScopesFromConfig([]byte(`{`)))

	specs := []DetectionScopeConfig{
		{Category: "prompt_injection", ScopeInclude: `kind == "user_message"`, ScopeExempt: ""},
		{Category: "cli_destructive", ScopeInclude: "", ScopeExempt: ""},
	}
	out, err := WithDetectionScopes(nil, specs)
	require.NoError(t, err)
	require.JSONEq(t, `{"detection_scopes":[{"category":"prompt_injection","scope_include":"kind == \"user_message\""},{"category":"cli_destructive"}]}`, string(out))
	require.Equal(t, specs, DetectionScopesFromConfig(out))

	out, err = WithDetectionScopes(out, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(out))
}

func TestCategoryScopesApplyTheRecommendation(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{msg(message.Assistant), msg(message.User)}
	masks := NewCategoryScopes(mustRecommendedSet(t), nil).Masks(t.Context(), messages)
	// The secrets recommendation exempts assistant messages.
	require.False(t, masks.InScope(0, categories.CategorySecrets))
	require.True(t, masks.InScope(1, categories.CategorySecrets))
}

func TestPromptPolicyUsesPromptPolicyCategoryMask(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{msg(message.User)}
	masks := masksFor(t, nil, messages)
	require.True(t, masks.InScope(0, categories.CategoryPromptPolicy))
	require.True(t, masks.AdmitsAny(0, sourceCategories[promptpolicy.Source]))
}
