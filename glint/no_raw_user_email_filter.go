package glint

// no-raw-user-email-filter forbids matching or bucketing the email dimension
// on raw user_email in squirrel-built ClickHouse queries. Employee identity is
// folded through the identity_map at read time (joinGet — see
// server/internal/telemetry/repo/canonical_identity.go); a raw comparison or
// GROUP BY splits one employee across their work, personal, and case-variant
// emails, which is the regression class behind DNO-827/DNO-425 and the reason
// the canonical fold exists. Squirrel is only permitted for ClickHouse in this
// repository (Postgres must use sqlc), so squirrel usage is the precise scope
// of the rule; SELECT projections are deliberately out of scope — reading the
// email for display is fine, matching on it is not.

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	noRawUserEmailFilterAnalyzer       = "norawuseremailfilter"
	noRawUserEmailFilterDefaultMessage = "route email matching through the canonical identity fold (canonicalEmailExpr / joinGet over identity_map) instead of raw user_email — raw matching splits one employee across their linked emails"
)

const squirrelPkgPath = "github.com/Masterminds/squirrel"

// squirrelFilterMethods are the SelectBuilder methods that express matching or
// bucketing. Select/Column projections are intentionally absent.
var squirrelFilterMethods = map[string]bool{
	"Where":      true,
	"Having":     true,
	"GroupBy":    true,
	"JoinClause": true,
}

type noRawUserEmailFilterSettings struct {
	Disabled bool `json:"disabled"`
}

func newNoRawUserEmailFilterAnalyzer(_ noRawUserEmailFilterSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     noRawUserEmailFilterAnalyzer,
		Doc:      noRawUserEmailFilterDefaultMessage,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run:      runNoRawUserEmailFilter,
	}
}

func runNoRawUserEmailFilter(pass *analysis.Pass) (any, error) {
	ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	reported := make(map[token.Pos]bool)

	ins.Preorder([]ast.Node{(*ast.CallExpr)(nil), (*ast.CompositeLit)(nil)}, func(node ast.Node) {
		switch n := node.(type) {
		case *ast.CallExpr:
			if isSquirrelFilterCall(pass, n) || isSquirrelExprCall(pass, n) {
				// Only the predicate/SQL arguments carry column expressions.
				// For Where/Having/JoinClause/Expr that is the first argument;
				// the rest are bound values — data that may legitimately
				// contain "user_email". GroupBy is variadic SQL throughout.
				args := n.Args
				if !isGroupByCall(n) && len(args) > 1 {
					args = args[:1]
				}
				for _, arg := range args {
					reportRawUserEmailLiterals(pass, arg, reported)
				}
			}
		case *ast.CompositeLit:
			// squirrel.Eq / squirrel.NotEq map keys are column expressions
			// wherever the literal appears, including inside helpers that
			// return predicates for a later Where.
			if isSquirrelMapType(pass.TypesInfo.TypeOf(n)) {
				for _, elt := range n.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						reportRawUserEmailLiterals(pass, kv.Key, reported)
					}
				}
			}
		}
	})

	return nil, nil
}

func isGroupByCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "GroupBy"
}

// isSquirrelFilterCall reports whether call is a matching/bucketing method on
// a squirrel builder.
func isSquirrelFilterCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !squirrelFilterMethods[sel.Sel.Name] {
		return false
	}
	return isSquirrelType(pass.TypesInfo.TypeOf(sel.X))
}

// isSquirrelExprCall reports whether call is squirrel.Expr(...), the escape
// hatch every hand-written predicate goes through.
func isSquirrelExprCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Expr" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	pkgName, ok := pass.TypesInfo.Uses[ident].(*types.PkgName)
	return ok && pkgName.Imported().Path() == squirrelPkgPath
}

func isSquirrelType(t types.Type) bool {
	if t == nil {
		return false
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == squirrelPkgPath
}

func isSquirrelMapType(t types.Type) bool {
	if !isSquirrelType(t) {
		return false
	}
	named := t.(*types.Named)
	name := named.Obj().Name()
	return name == "Eq" || name == "NotEq"
}

// reportRawUserEmailLiterals walks an expression and reports every string
// literal that names user_email. Descent stops at calls to the canonical fold
// helpers (canonical*): a literal handed to them is the sanctioned path, and
// the emitted SQL folds it through joinGet.
func reportRawUserEmailLiterals(pass *analysis.Pass, expr ast.Expr, reported map[token.Pos]bool) {
	ast.Inspect(expr, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.CallExpr:
			// Matching the funnel by name prefix is deliberate: the remaining
			// canonical-fold flips keep adding canonical* helpers, and a
			// type-pinned function list would need updating for each one. The
			// trade-off is that any function named canonical* is trusted to
			// fold — do not name a helper canonical* unless it routes through
			// the identity_map.
			if calleeName(e) != "" && strings.HasPrefix(strings.ToLower(calleeName(e)), "canonical") {
				return false
			}
		case *ast.CompositeLit:
			// squirrel map values are filter DATA, not column expressions: a
			// value that happens to equal "user_email" (e.g. querying for a
			// dimension by that name) is not a violation. Scan keys only.
			if isSquirrelMapType(pass.TypesInfo.TypeOf(e)) {
				for _, elt := range e.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						reportRawUserEmailLiterals(pass, kv.Key, reported)
					}
				}
				return false
			}
		case *ast.BasicLit:
			if e.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(e.Value)
			if err != nil {
				return true
			}
			if !strings.Contains(value, "user_email") || strings.Contains(value, "joinGet('identity_map'") {
				return true
			}
			if reported[e.Pos()] {
				return true
			}
			reported[e.Pos()] = true
			pass.ReportRangef(e, "%s", noRawUserEmailFilterDefaultMessage)
		}
		return true
	})
}

func calleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}
