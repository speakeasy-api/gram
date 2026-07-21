package risk_analysis

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
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
	promptpolicy.Source:             {categories.CategoryPromptPolicy},
}

// SourceCategories returns the categories a detector source can emit.
func SourceCategories(source string) []categories.Category {
	cats := sourceCategories[source]
	out := make([]categories.Category, len(cats))
	copy(out, cats)
	return out
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

// CompileDetectionScopes compiles a policy's specified per-category detection
// scopes. A specified category replaces its registry recommendation; a scope
// with no predicates (both CEL fields empty) is inert and admits every
// message surface.
func CompileDetectionScopes(eng *celenv.Engine, specs []DetectionScopeConfig) (map[categories.Category]CompiledScope, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make(map[categories.Category]CompiledScope, len(specs))
	for _, spec := range specs {
		scope, err := CompileScope(eng, spec.ScopeInclude, spec.ScopeExempt)
		if err != nil {
			return nil, fmt.Errorf("compile detection scope %s: %w", spec.Category, err)
		}
		out[categories.Category(spec.Category)] = scope
	}
	return out, nil
}

// effectiveScope resolves a category's detection scope: the policy-specified
// scope wins over the registry recommendation.
func effectiveScope(rec RecommendedSet, specified map[categories.Category]CompiledScope, cat categories.Category) (CompiledScope, bool) {
	if scope, ok := specified[cat]; ok {
		return scope, true
	}
	return rec.scope(cat)
}

// CategoryScopes is the per-batch composition of per-category detection
// scopes: policy-specified scopes merged over registry recommendations, with
// the specified scope winning per category.
type CategoryScopes struct {
	rec       RecommendedSet
	specified map[categories.Category]CompiledScope
	metrics   *riskMetrics
}

// CategoryScopeMasks contains per-message category scope exclusions.
type CategoryScopeMasks struct {
	categoryOut map[categories.Category][]bool
}

func NewCategoryScopes(rec RecommendedSet, specified map[categories.Category]CompiledScope, metrics *riskMetrics) CategoryScopes {
	return CategoryScopes{
		rec:       rec,
		specified: specified,
		metrics:   metrics,
	}
}

// CategoryScope is the single-message composition used by realtime
// enforcement: policy-specified detection scopes merged over registry
// recommendations, specified winning per category.
type CategoryScope struct {
	rec       RecommendedSet
	specified map[categories.Category]CompiledScope
}

// NewCategoryScope builds a single-message category scope. Its zero value
// admits every message.
func NewCategoryScope(rec RecommendedSet, specified map[categories.Category]CompiledScope) CategoryScope {
	return CategoryScope{
		rec:       rec,
		specified: specified,
	}
}

// InScope reports whether view is in scope for cat.
func (s CategoryScope) InScope(view MessageView, cat categories.Category) bool {
	scope, ok := effectiveScope(s.rec, s.specified, cat)
	if !ok {
		return true
	}
	return scope.Includes(view) && !scope.Exempts(view)
}

// SourceInScope reports whether view is in scope for at least one category the
// source can emit. Unknown sources are admitted so new scanner sources do not
// fail closed before they are added to sourceCategories.
func (s CategoryScope) SourceInScope(view MessageView, source string) bool {
	cats := SourceCategories(source)
	if len(cats) == 0 {
		return true
	}
	for _, cat := range cats {
		if s.InScope(view, cat) {
			return true
		}
	}
	return false
}

// FilterFindings drops findings whose classified category is out of scope.
func (s CategoryScope) FilterFindings(view MessageView, findings []scanners.Finding) []scanners.Finding {
	if len(findings) == 0 {
		return findings
	}
	out := make([]scanners.Finding, 0, len(findings))
	for _, finding := range findings {
		if s.InScope(view, categoryForFinding(finding)) {
			out = append(out, finding)
		}
	}
	return out
}

// Masks computes the per-category detection scope masks for the batch.
// Identical CEL programs are evaluated once per message and shared by every
// category using that program.
func (s CategoryScopes) Masks(_ context.Context, messages []batchMessage) CategoryScopeMasks {
	masks := CategoryScopeMasks{
		categoryOut: map[categories.Category][]bool{},
	}
	if len(s.rec.scopes) == 0 && len(s.specified) == 0 {
		return masks
	}

	effective := make(map[categories.Category]CompiledScope, len(s.rec.scopes)+len(s.specified))
	maps.Copy(effective, s.rec.scopes)
	maps.Copy(effective, s.specified)

	type evalKey struct {
		include string
		exempt  string
	}
	evaluated := map[evalKey][]bool{}
	for cat, scope := range effective {
		if !scope.Active() {
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

func (m CategoryScopeMasks) InScope(i int, cat categories.Category) bool {
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
	// Without category masks the prefilter is a no-op.
	if len(m.categoryOut) == 0 {
		indices := make([]int, len(messages))
		for i := range indices {
			indices[i] = i
		}
		return messages, contents, indices
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
