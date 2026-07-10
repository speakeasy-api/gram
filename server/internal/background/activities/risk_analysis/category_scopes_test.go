package risk_analysis

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/accountidentity"
	"github.com/speakeasy-api/gram/server/internal/scanners/destructivetool"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
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
		ID:           uuid.New(),
		Type:         typ,
		Content:      "content",
		RawToolCalls: nil,
		ToolCalls:    []recordedToolCall{},
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

func masksFor(t *testing.T, enabled bool, disabled []string, messages []batchMessage) CategoryScopeMasks {
	t.Helper()
	return NewCategoryScopes(CompiledScope{eng: nil, include: nil, exempt: nil, includeCEL: "", exemptCEL: ""}, mustRecommendedSet(t), disabled, enabled, nil).Masks(t.Context(), messages)
}

func mergeOne(masks CategoryScopeMasks, findings [][]scanners.Finding) [][]scanners.Finding {
	return mergeFindings(mergeFindingsInput{
		orgID:                   "",
		metrics:                 nil,
		masks:                   masks,
		exclusions:              NewExclusionSet(nil),
		builtinEnabled:          false,
		builtinPresets:          nil,
		gitleaksFindings:        findings,
		presidioFindings:        make([][]scanners.Finding, len(findings)),
		shadowMCPFindings:       make([][]scanners.Finding, len(findings)),
		destructiveToolFindings: make([][]scanners.Finding, len(findings)),
		cliDestructiveFindings:  make([][]scanners.Finding, len(findings)),
		promptInjectionFindings: make([][]scanners.Finding, len(findings)),
		customFindings:          make([][]scanners.Finding, len(findings)),
	}, nil)
}

func TestRecommendedCategoryScopesPromptInjectionBehavior(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{
		msg(message.Assistant),
		msg(message.ToolResponse),
		msg(message.User),
	}
	pi := finding(SourcePromptInjection, promptinjection.Rule)

	on := masksFor(t, true, nil, messages)
	require.False(t, on.InScope(0, categories.CategoryPromptInjection))
	require.True(t, on.InScope(1, categories.CategoryPromptInjection))
	require.True(t, on.InScope(2, categories.CategoryPromptInjection))

	off := masksFor(t, false, nil, messages)
	require.True(t, off.InScope(0, categories.CategoryPromptInjection))

	findings := [][]scanners.Finding{{pi}, {pi}, {pi}}
	require.Empty(t, mergeOne(on, findings)[0])
	require.Len(t, mergeOne(on, findings)[1], 1)
	require.Equal(t, mergeOne(CategoryScopeMasks{policyOut: nil, categoryOut: nil}, findings), mergeOne(off, findings))
}

func TestRecommendedCategoryScopesMultiSourceSameMessage(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{msg(message.Assistant)}
	masks := masksFor(t, true, nil, messages)
	pi := finding(SourcePromptInjection, promptinjection.Rule)
	secret := finding(SourceGitleaks, "secret.generic-api-key")

	out := mergeOne(masks, [][]scanners.Finding{{pi, secret}})
	require.Len(t, out[0], 1)
	require.Equal(t, SourceGitleaks, out[0][0].Source)
}

func TestRecommendedCategoryScopesOptOutKeepsPromptInjection(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{msg(message.Assistant)}
	masks := masksFor(t, true, []string{string(categories.CategoryPromptInjection)}, messages)

	out := mergeOne(masks, [][]scanners.Finding{{finding(SourcePromptInjection, promptinjection.Rule)}})
	require.Len(t, out[0], 1)
}

func TestRecommendedCategoryScopesToolRequestOnlyCategories(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{
		toolReq("Bash"),
		msg(message.Assistant),
	}
	masks := masksFor(t, true, nil, messages)
	require.True(t, masks.InScope(0, categories.CategoryCLIDestructive))
	require.False(t, masks.InScope(1, categories.CategoryCLIDestructive))

	cli := finding(SourceCLIDestructive, "cli_destructive.rm_rf")
	out := mergeOne(masks, [][]scanners.Finding{{cli}, {cli}})
	require.Len(t, out[0], 1)
	require.Empty(t, out[1])
}

func TestRecommendedCategoryScopesAccountIdentityUntouched(t *testing.T) {
	t.Parallel()

	rec := mustRecommendedSet(t)
	_, ok := rec.scope(categories.CategoryAccountIdentity)
	require.False(t, ok)

	messageID := uuid.New()
	ids, findings := mergeSessionFindings(
		[]uuid.UUID{},
		[][]scanners.Finding{},
		[]sessionFinding{{
			messageID: messageID,
			findings:  []scanners.Finding{finding(accountidentity.Source, "identity.unapproved_domain")},
		}},
		NewExclusionSet(nil),
	)
	require.Equal(t, []uuid.UUID{messageID}, ids)
	require.Len(t, findings, 1)
	require.Len(t, findings[0], 1)
}

func TestRecommendedCategoryScopesSubset(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{
		msg(message.Assistant),
		msg(message.ToolResponse),
		toolReq("Read"),
		toolReq("Bash"),
	}
	contents := messageContents(messages)
	masks := masksFor(t, true, nil, messages)
	require.Equal(t, 2, masks.RecommendedPrefilteredCount(sourceCategories[SourcePromptInjection]))
	require.Equal(t, 0, masks.RecommendedPrefilteredCount(sourceCategories[SourcePresidio]))

	piMessages, piContents, piIndices := masks.Subset(messages, contents, sourceCategories[SourcePromptInjection])
	require.Equal(t, []int{1, 3}, piIndices)
	require.Len(t, piMessages, 2)
	require.Equal(t, []string{contents[1], contents[3]}, piContents)

	presidioMessages, _, presidioIndices := masks.Subset(messages, contents, sourceCategories[SourcePresidio])
	require.Equal(t, []int{0, 1, 2, 3}, presidioIndices)
	require.Len(t, presidioMessages, 4)
}

func TestSourceCategoriesConsistentWithClassify(t *testing.T) {
	t.Parallel()

	representative := map[categories.Category]scanners.Finding{
		categories.CategoryAccountIdentity: finding(accountidentity.Source, "identity.unapproved_domain"),
		categories.CategorySecrets:         finding(SourceGitleaks, "secret.generic-api-key"),
		categories.CategoryFinancial:       finding(SourcePresidio, "pii.credit_card"),
		categories.CategoryGovernmentIDs:   finding(SourcePresidio, "pii.us_ssn"),
		categories.CategoryHealthcare:      finding(SourcePresidio, "pii.us_mbi"),
		categories.CategoryOffPolicy:       finding(SourcePresidio, "pii.policy_violation"),
		categories.CategoryPII:             finding(SourcePresidio, "pii.email_address"),
		categories.CategoryPromptInjection: finding(SourcePromptInjection, promptinjection.Rule),
		categories.CategoryShadowMCP:       finding(shadowmcp.SourceShadowMCP, "shadow_mcp"),
		categories.CategoryDestructiveTool: finding(shadowmcp.SourceDestructiveTool, destructivetool.Rule),
		categories.CategoryCLIDestructive:  finding(SourceCLIDestructive, "cli_destructive.rm_rf"),
		categories.CategoryPromptPolicy:    finding(SourceLLMJudge, RuleLLMJudge),
		categories.CategoryCustom:          finding(SourceCustom, "custom.test"),
	}

	reachable := map[categories.Category]bool{}
	for source, cats := range sourceCategories {
		require.NotEmpty(t, cats)
		for _, cat := range cats {
			rep, ok := representative[cat]
			require.Truef(t, ok, "missing representative for %s", cat)
			require.Equalf(t, source, rep.Source, "representative source for %s", cat)
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

func TestDisabledRecommendedScopesConfig(t *testing.T) {
	t.Parallel()

	require.Nil(t, DisabledRecommendedScopesFromConfig(nil))
	require.Nil(t, DisabledRecommendedScopesFromConfig([]byte(`{`)))

	out, err := WithDisabledRecommendedScopes(nil, []string{"prompt_injection", "cli_destructive"})
	require.NoError(t, err)
	require.JSONEq(t, `{"recommended_scopes":{"disabled_categories":["prompt_injection","cli_destructive"]}}`, string(out))
	require.Equal(t, []string{"prompt_injection", "cli_destructive"}, DisabledRecommendedScopesFromConfig(out))

	out, err = WithDisabledRecommendedScopes(out, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(out))
}

func TestCategoryScopesPolicyScopeStillApplies(t *testing.T) {
	t.Parallel()

	eng, err := celenv.New()
	require.NoError(t, err)
	policy, err := CompileScope(eng, `kind == "user_message"`, "")
	require.NoError(t, err)

	messages := []batchMessage{msg(message.Assistant), msg(message.User)}
	masks := NewCategoryScopes(policy, mustRecommendedSet(t), nil, false, nil).Masks(t.Context(), messages)
	require.False(t, masks.InScope(0, categories.CategorySecrets))
	require.True(t, masks.InScope(1, categories.CategorySecrets))
}

func TestPromptPolicyUsesPromptPolicyCategoryMask(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{msg(message.User)}
	masks := masksFor(t, true, nil, messages)
	require.True(t, masks.InScope(0, categories.CategoryPromptPolicy))
	require.True(t, masks.AdmitsAny(0, sourceCategories[SourceLLMJudge]))
}

func TestCategoryScopesDoesNotAffectCustomRegistryScope(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{msg(message.Assistant)}
	masks := masksFor(t, true, nil, messages)
	custom := finding(SourceCustom, "custom.rule")

	out := mergeFindings(mergeFindingsInput{
		orgID:                   "",
		metrics:                 nil,
		masks:                   masks,
		exclusions:              NewExclusionSet(nil),
		builtinEnabled:          false,
		builtinPresets:          nil,
		gitleaksFindings:        make([][]scanners.Finding, 1),
		presidioFindings:        make([][]scanners.Finding, 1),
		shadowMCPFindings:       make([][]scanners.Finding, 1),
		destructiveToolFindings: make([][]scanners.Finding, 1),
		cliDestructiveFindings:  make([][]scanners.Finding, 1),
		promptInjectionFindings: make([][]scanners.Finding, 1),
		customFindings:          [][]scanners.Finding{{custom}},
	}, nil)
	require.Len(t, out[0], 1)
}
