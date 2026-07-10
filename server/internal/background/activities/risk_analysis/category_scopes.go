package risk_analysis

import (
	"context"
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// sourceCategories records which finding categories each detector source can
// emit. It is intentionally broader than any one current rule so pre-filtering
// never drops a category a source may produce.
var sourceCategories = map[string][]categories.Category{
	SourceGitleaks:                  {categories.CategorySecrets},
	SourcePresidio:                  {categories.CategoryFinancial, categories.CategoryGovernmentIDs, categories.CategoryHealthcare, categories.CategoryOffPolicy, categories.CategoryPII},
	SourcePromptInjection:           {categories.CategoryPromptInjection},
	shadowmcp.SourceShadowMCP:       {categories.CategoryShadowMCP},
	shadowmcp.SourceDestructiveTool: {categories.CategoryDestructiveTool},
	SourceCLIDestructive:            {categories.CategoryCLIDestructive},
	SourceCustom:                    {categories.CategoryCustom},
	SourceLLMJudge:                  {categories.CategoryPromptPolicy},
}

// RecommendedSet holds the registry's scopes compiled once against the shared
// celenv engine. Built in the AnalyzeBatch constructor; a bad registry entry
// fails fast at worker boot.
type RecommendedSet struct {
	scopes map[categories.Category]CompiledScope
}

// CompileRecommended compiles the centrally maintained category scope registry.
func CompileRecommended(eng *celenv.Engine) (RecommendedSet, error) {
	out := RecommendedSet{scopes: map[categories.Category]CompiledScope{}}
	for _, rec := range recommendedscopes.All() {
		if !rec.Applicable || (rec.ScopeInclude == "" && rec.ScopeExempt == "") {
			continue
		}
		scope, err := CompileScope(eng, rec.ScopeInclude, rec.ScopeExempt)
		if err != nil {
			return RecommendedSet{scopes: nil}, fmt.Errorf("compile recommended scope %s: %w", rec.Category, err)
		}
		out.scopes[rec.Category] = scope
	}
	return out, nil
}

func (r RecommendedSet) scope(cat categories.Category) (CompiledScope, bool) {
	if len(r.scopes) == 0 {
		return CompiledScope{eng: nil, include: nil, exempt: nil, includeCEL: "", exemptCEL: ""}, false
	}
	scope, ok := r.scopes[cat]
	return scope, ok
}

// CategoryScopes is the per-batch composition: policy scope ∩ (recommended
// scopes − policy opt-outs). Nil-safe zero value = policy scope only.
type CategoryScopes struct {
	policy   CompiledScope
	rec      RecommendedSet
	disabled map[categories.Category]bool
	enabled  bool
	metrics  *riskMetrics
}

// CategoryScopeMasks contains per-message policy and category scope exclusions.
type CategoryScopeMasks struct {
	policyOut   []bool
	categoryOut map[categories.Category][]bool
}

func NewCategoryScopes(policy CompiledScope, rec RecommendedSet, disabled []string, enabled bool, metrics *riskMetrics) CategoryScopes {
	disabledSet := make(map[categories.Category]bool, len(disabled))
	for _, cat := range disabled {
		disabledSet[categories.Category(cat)] = true
	}
	return CategoryScopes{
		policy:   policy,
		rec:      rec,
		disabled: disabledSet,
		enabled:  enabled,
		metrics:  metrics,
	}
}

// Masks computes the policy-scope mask and category-specific recommended-scope
// masks for the batch. Identical recommended CEL programs are evaluated once
// per message and shared by every category using that program.
func (s CategoryScopes) Masks(_ context.Context, messages []batchMessage) CategoryScopeMasks {
	masks := CategoryScopeMasks{
		policyOut:   s.policyExclusions(messages),
		categoryOut: map[categories.Category][]bool{},
	}
	if !s.enabled || len(s.rec.scopes) == 0 {
		return masks
	}

	type evalKey struct {
		include string
		exempt  string
	}
	evaluated := map[evalKey][]bool{}
	for cat, scope := range s.rec.scopes {
		if s.disabled[cat] {
			continue
		}
		key := evalKey{include: scope.includeCEL, exempt: scope.exemptCEL}
		out, ok := evaluated[key]
		if !ok {
			out = make([]bool, len(messages))
			for i, msg := range messages {
				view := batchMessageView(msg)
				out[i] = !scope.Includes(view) || scope.Exempts(view)
			}
			evaluated[key] = out
		}
		masks.categoryOut[cat] = out
	}
	return masks
}

func (s CategoryScopes) policyExclusions(messages []batchMessage) []bool {
	if !s.policy.Active() {
		return []bool{}
	}
	excluded := make([]bool, len(messages))
	for i, msg := range messages {
		view := batchMessageView(msg)
		excluded[i] = !s.policy.Includes(view) || s.policy.Exempts(view)
	}
	return excluded
}

func (m CategoryScopeMasks) InScope(i int, cat categories.Category) bool {
	if len(m.policyOut) > 0 && m.policyOut[i] {
		return false
	}
	if categoryOut, ok := m.categoryOut[cat]; ok && categoryOut[i] {
		return false
	}
	return true
}

func (m CategoryScopeMasks) AdmitsAny(i int, cats []categories.Category) bool {
	for _, cat := range cats {
		if m.InScope(i, cat) {
			return true
		}
	}
	return false
}

func (m CategoryScopeMasks) Subset(messages []batchMessage, contents []string, cats []categories.Category) ([]batchMessage, []string, []int) {
	if len(messages) == 0 {
		return nil, nil, nil
	}
	subMessages := make([]batchMessage, 0, len(messages))
	subContents := make([]string, 0, len(contents))
	indices := make([]int, 0, len(messages))
	for i, msg := range messages {
		if !m.AdmitsAny(i, cats) {
			continue
		}
		subMessages = append(subMessages, msg)
		subContents = append(subContents, contents[i])
		indices = append(indices, i)
	}
	return subMessages, subContents, indices
}

func (m CategoryScopeMasks) RecommendedPrefilteredCount(cats []categories.Category) int {
	count := 0
	for _, out := range m.categoryOut {
		if len(out) > count {
			count = len(out)
		}
	}
	if count == 0 {
		return 0
	}

	skipped := 0
	for i := range count {
		if len(m.policyOut) > 0 && m.policyOut[i] {
			continue
		}
		if m.AdmitsAny(i, cats) {
			continue
		}
		skipped++
	}
	return skipped
}

func scatterFindings(n int, indices []int, subset [][]scanners.Finding) [][]scanners.Finding {
	out := make([][]scanners.Finding, n)
	for i, idx := range indices {
		if i < len(subset) {
			out[idx] = subset[i]
		}
	}
	return out
}

func categoryForFinding(f scanners.Finding) categories.Category {
	return categories.Classify(f.Source, f.RuleID)
}

func sourceCanEmit(source string, cat categories.Category) bool {
	return slices.Contains(sourceCategories[source], cat)
}
