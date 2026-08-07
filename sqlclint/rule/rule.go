// Package rule evaluates the tenancy-scoping rule against sqlc queries.
package rule

import (
	"fmt"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/sqlclint/annotation"
	"github.com/speakeasy-api/gram/sqlclint/catalog"
	"github.com/speakeasy-api/gram/sqlclint/query"
	"github.com/speakeasy-api/gram/sqlclint/schema"
)

// Diagnostic is one reported problem.
type Diagnostic struct {
	// RuleID is the catalog id, so the reader can run `sqlclint rule <id>`.
	RuleID string

	// File is the query file the problem was found in.
	File string

	// Line is the 1-indexed line to anchor the report at.
	Line int

	// Query is the sqlc query name.
	Query string

	// Message states the problem in one sentence.
	Message string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s:%d: [%s] query %q: %s", d.File, d.Line, d.RuleID, d.Query, d.Message)
}

// Engine evaluates queries against a schema and the rule catalog.
type Engine struct {
	classifier *schema.Classifier
	catalog    *catalog.Catalog
}

// NewEngine returns an Engine bound to a schema classification and a catalog.
func NewEngine(classifier *schema.Classifier, cat *catalog.Catalog) *Engine {
	return &Engine{classifier: classifier, catalog: cat}
}

// Result is the outcome of checking one query.
type Result struct {
	// Query is the query that was checked.
	Query query.Query

	// Scope holds problems with the query's tenancy bound. These are the only
	// diagnostics the ignore file may grandfather, because they are the only
	// ones that represent a debt rather than a mistake.
	Scope []Diagnostic

	// Structural holds problems with the annotation, the ignore file, or the
	// check itself. Each has a direct fix, so none is ever grandfathered and all
	// are reported unconditionally.
	Structural []Diagnostic

	// Exempted reports whether a valid annotation silenced a scope diagnostic.
	Exempted bool

	// Scoped reports whether the query met the tenancy rule on its own, without
	// needing an annotation. It drives redundant-exemption.
	Scoped bool
}

// Diagnostics returns every problem found, structural ones first.
func (r Result) Diagnostics() []Diagnostic {
	return append(append([]Diagnostic{}, r.Structural...), r.Scope...)
}

// Check evaluates one query.
func (e *Engine) Check(q query.Query) Result {
	res := Result{Query: q, Scope: nil, Structural: nil, Exempted: false, Scoped: false}

	ann, hasAnn := annotation.Find(q.Comments, q.Line+1)
	if hasAnn {
		switch {
		case ann.Category == "" || !e.catalog.IsExemption(ann.Category):
			res.Structural = append(res.Structural, Diagnostic{
				RuleID:  catalog.UnknownExemptionCategory,
				File:    q.File,
				Line:    ann.Line,
				Query:   q.Name,
				Message: fmt.Sprintf("%q is not an exemption category; valid categories are %s", ann.Category, strings.Join(e.catalog.IDs(catalog.KindExemption), ", ")),
			})
			// The category is unusable, so it cannot silence anything. Fall
			// through and report the scope problem too.
			hasAnn = false
		case ann.Reason == "":
			res.Structural = append(res.Structural, Diagnostic{
				RuleID:  catalog.MissingExemptionReason,
				File:    q.File,
				Line:    ann.Line,
				Query:   q.Name,
				Message: fmt.Sprintf("the %s exemption has no reason; write \"-- %s %s -- <why this is true of this query>\"", ann.Category, annotation.Prefix, ann.Category),
			})
			hasAnn = false
		}
	}

	scopeDiags, scoped, global := e.checkScope(q)
	res.Scoped = scoped

	switch {
	case scoped && hasAnn:
		reason := "the query is already tenancy-scoped"
		if global {
			reason = "every table the query touches is global"
		}
		res.Structural = append(res.Structural, Diagnostic{
			RuleID:  catalog.RedundantExemption,
			File:    q.File,
			Line:    ann.Line,
			Query:   q.Name,
			Message: fmt.Sprintf("%s, so it is accepted without the %s exemption; delete the annotation", reason, ann.Category),
		})
	case hasAnn:
		res.Exempted = true
	default:
		res.Scope = append(res.Scope, scopeDiags...)
	}

	// A query that cannot be parsed or resolved is not debt to be tracked; it is
	// unverifiable, and must fail until someone makes it checkable.
	res.Scope, res.Structural = partitionStructural(res.Scope, res.Structural)

	return res
}

// partitionStructural moves the always-fatal diagnostics out of the scope set.
func partitionStructural(scope, structural []Diagnostic) ([]Diagnostic, []Diagnostic) {
	var kept []Diagnostic
	for _, d := range scope {
		switch d.RuleID {
		case catalog.UnparseableQuery, catalog.UnknownTableReference:
			structural = append(structural, d)
		default:
			kept = append(kept, d)
		}
	}
	return kept, structural
}

// checkScope evaluates the tenancy rule, returning any scope diagnostics and
// whether the query is scoped without needing an exemption.
func (e *Engine) checkScope(q query.Query) (diags []Diagnostic, scoped, global bool) {
	tree, err := q.Parse()
	if err != nil {
		return []Diagnostic{{
			RuleID:  catalog.UnparseableQuery,
			File:    q.File,
			Line:    q.SQLLine,
			Query:   q.Name,
			Message: err.Error(),
		}}, false, false
	}

	refs := collectReferences(tree)

	var reqs []schema.Requirement
	var unknown []string
	for _, t := range refs.tables {
		req, known := e.classifier.Require(t)
		if !known {
			unknown = append(unknown, t)
			continue
		}
		reqs = append(reqs, req)
	}

	if len(unknown) > 0 {
		return []Diagnostic{{
			RuleID:  catalog.UnknownTableReference,
			File:    q.File,
			Line:    q.SQLLine,
			Query:   q.Name,
			Message: fmt.Sprintf("%s is not a table in the schema and is not a CTE of this query, so its tenancy cannot be resolved", quoteList(unknown)),
		}}, false, false
	}

	required := schema.QueryRequirement(reqs)
	if required.Global() {
		// Nothing the query touches belongs to a tenant.
		return nil, true, true
	}

	b := collectBinds(tree)
	if required.Satisfies(b.columns()) {
		return nil, true, false
	}

	// The bound exists but can be switched off, which is a distinct failure from
	// having no bound at all.
	for _, c := range required {
		if b.nullable[c] {
			return []Diagnostic{{
				RuleID:  catalog.NullableTenantParam,
				File:    q.File,
				Line:    q.SQLLine,
				Query:   q.Name,
				Message: fmt.Sprintf("%s is bound with sqlc.narg, so passing NULL disables the tenancy boundary; bind it with @%s and keep any optional filter separate", c, c),
			}}, false, false
		}
	}

	// Some tenancy column is bound, just not one that bounds these tables.
	if b.any() {
		return []Diagnostic{{
			RuleID:  catalog.WrongTenantColumn,
			File:    q.File,
			Line:    q.SQLLine,
			Query:   q.Name,
			Message: fmt.Sprintf("binds %s, but %s requires %s", quoteList(boundNames(b)), quoteList(refs.tables), orList(required)),
		}}, false, false
	}

	return []Diagnostic{{
		RuleID:  catalog.MissingTenantScope,
		File:    q.File,
		Line:    q.SQLLine,
		Query:   q.Name,
		Message: fmt.Sprintf("touches %s without binding %s", quoteList(refs.tables), orList(required)),
	}}, false, false
}

func boundNames(b binds) []string {
	out := b.columns()
	for c := range b.nullable {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return out
}

func quoteList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, i := range items {
		quoted = append(quoted, quote(i))
	}
	return strings.Join(quoted, ", ")
}

func orList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, i := range items {
		quoted = append(quoted, quote(i))
	}
	if len(quoted) < 2 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}

func quote(s string) string { return "\"" + s + "\"" }
