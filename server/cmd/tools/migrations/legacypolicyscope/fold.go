// Package legacypolicyscope folds the legacy policy-level risk scope
// (message_types, scope_include, scope_exempt) into per-category detection
// scopes, leaving detection scopes as the only scoping surface.
//
// Both surfaces narrow, and the scanner intersects them: a message is scanned
// for a category only when the policy scope admits it AND the category scope
// admits it (see risk_analysis.CategoryScope.InScope). Dropping the legacy
// scope therefore widens what a policy scans, so the fold is conditional on
// what the policy does on a match:
//
//   - enforcing (warn, block, quarantine): compose the legacy scope into each
//     category scope, so enforcement is unchanged. Costs those categories their
//     link to the recommendation registry, which is why it is not done to every
//     policy.
//   - flag: drop the legacy scope. The policy widens, which only produces more
//     findings, and the categories keep tracking the registry.
package legacypolicyscope

import (
	"fmt"
	"slices"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
)

// ErrNoCategories reports an enforcing policy whose categories could not be
// resolved. Folding it would drop its legacy narrowing and silently widen
// enforcement, so the runner stops rather than guessing.
var ErrNoCategories = fmt.Errorf("enforcing policy resolves to no detection categories")

// Disposition is what the fold did to one policy.
type Disposition string

const (
	DispositionPreserved Disposition = "preserved"
	DispositionCleared   Disposition = "cleared"
	DispositionNoop      Disposition = "noop"
)

// Policy is the subset of a risk_policies row the fold reads.
type Policy struct {
	Action          string
	PolicyType      string
	Sources         []string
	CustomRuleIDs   []string
	MessageTypes    []string
	ScopeInclude    string
	ScopeExempt     string
	DetectionScopes []ra.DetectionScopeConfig
}

// Result is the folded outcome. DetectionScopes is the complete desired set,
// sorted by category, and is written verbatim to analyzer_config.
type Result struct {
	Disposition     Disposition
	DetectionScopes []ra.DetectionScopeConfig
}

// enforcing reports whether a match interrupts the caller. "warn" denies the
// current call and returns an acknowledgement link, so it interrupts as surely
// as block does and must not widen underneath the user.
func enforcing(action string) bool {
	return action == "warn" || action == "block" || action == "quarantine"
}

// hasLegacyScope reports whether the row still carries a legacy narrowing.
func (p Policy) hasLegacyScope() bool {
	return len(p.MessageTypes) > 0 || p.ScopeInclude != "" || p.ScopeExempt != ""
}

// Fold computes the detection scopes that replace p's legacy policy scope.
func Fold(p Policy, catalog policycatalog.Catalog) (Result, error) {
	if !p.hasLegacyScope() {
		return Result{Disposition: DispositionNoop, DetectionScopes: p.DetectionScopes}, nil
	}
	if !enforcing(p.Action) {
		return Result{Disposition: DispositionCleared, DetectionScopes: p.DetectionScopes}, nil
	}

	legacyInclude, err := legacyIncludeExpr(p, catalog)
	if err != nil {
		return Result{Disposition: "", DetectionScopes: nil}, err
	}

	cats := policyCategories(p)
	if len(cats) == 0 {
		return Result{Disposition: "", DetectionScopes: nil}, ErrNoCategories
	}

	// Carry through scopes for categories the policy no longer selects rather
	// than dropping rows the fold does not understand.
	out := make(map[string]ra.DetectionScopeConfig, len(p.DetectionScopes)+len(cats))
	for _, scope := range p.DetectionScopes {
		out[scope.Category] = scope
	}

	for _, cat := range cats {
		rec, hasRec := recommendedscopes.For(cat)
		if hasRec && !rec.Applicable {
			// Session-scoped category: message scoping never applied to it, so
			// there is nothing to compose the legacy narrowing into.
			continue
		}
		base := ra.DetectionScopeConfig{Category: string(cat), ScopeInclude: "", ScopeExempt: ""}
		if specified, ok := out[string(cat)]; ok {
			base = specified
		} else if hasRec {
			base.ScopeInclude = rec.ScopeInclude
			base.ScopeExempt = rec.ScopeExempt
		}
		out[string(cat)] = ra.DetectionScopeConfig{
			Category:     string(cat),
			ScopeInclude: intersectExprs(legacyInclude, base.ScopeInclude),
			ScopeExempt:  unionExprs(p.ScopeExempt, base.ScopeExempt),
		}
	}

	scopes := make([]ra.DetectionScopeConfig, 0, len(out))
	for _, scope := range out {
		scopes = append(scopes, scope)
	}
	slices.SortFunc(scopes, func(a, b ra.DetectionScopeConfig) int {
		if a.Category < b.Category {
			return -1
		}
		if a.Category > b.Category {
			return 1
		}
		return 0
	})
	return Result{Disposition: DispositionPreserved, DetectionScopes: scopes}, nil
}

// legacyIncludeExpr renders message_types and scope_include as one include
// predicate. message_types is a kind allowlist, which is exactly what
// policycatalog encodes, so the emitted CEL matches what the editor round-trips.
func legacyIncludeExpr(p Policy, catalog policycatalog.Catalog) (string, error) {
	kinds := ""
	if len(p.MessageTypes) > 0 {
		encoded, err := policycatalog.EncodeDetectionScope(p.MessageTypes, catalog)
		if err != nil {
			return "", fmt.Errorf("encode legacy message types: %w", err)
		}
		kinds = encoded
	}
	return intersectExprs(kinds, p.ScopeInclude), nil
}

// policyCategories resolves every category the policy can produce findings for.
func policyCategories(p Policy) []categories.Category {
	seen := map[categories.Category]bool{}
	var out []categories.Category
	add := func(cat categories.Category) {
		if seen[cat] {
			return
		}
		seen[cat] = true
		out = append(out, cat)
	}
	for _, source := range p.Sources {
		for _, cat := range ra.SourceCategories(source) {
			add(cat)
		}
	}
	if len(p.CustomRuleIDs) > 0 {
		add(categories.CategoryCustom)
	}
	if p.PolicyType == ra.PolicyTypePromptBased {
		for _, cat := range ra.SourceCategories(promptpolicy.Source) {
			add(cat)
		}
	}
	slices.Sort(out)
	return out
}

// intersectExprs ANDs two include predicates; an empty predicate admits
// everything and so drops out of the conjunction.
func intersectExprs(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return "(" + a + ") && (" + b + ")"
	}
}

// unionExprs ORs two exempt predicates; either one takes the message out.
func unionExprs(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return "(" + a + ") || (" + b + ")"
	}
}
