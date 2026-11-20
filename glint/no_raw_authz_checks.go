package glint

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// no-raw-authz-checks keeps migrated packages on the typed authorization
// requests generated from the authz registry (authz.MCPServer(id, project).Write(),
// authz.Environments(p).Write(), ...). A hand-built authz.Check
// literal or a scope-taking constructor such as authz.EnvironmentCheck lets a
// caller pair any scope with any resource and leave out dimensions the model
// requires; the refs make those mistakes unrepresentable.
//
// The rule applies to the packages in noRawAuthzChecksPackages, which grows
// as packages migrate. Once every package has migrated the list goes away and
// the rule applies everywhere outside internal/authz.

const (
	noRawAuthzChecksAnalyzer       = "norawauthzchecks"
	noRawAuthzChecksDefaultMessage = "build authorization checks with the typed refs generated from the authz registry, such as authz.Environment(id, project).Read()"
)

const authzPkgPath = "github.com/speakeasy-api/gram/server/internal/authz"

// noRawAuthzChecksPackages are the packages migrated to typed refs. Prefix
// matched, so subpackages inherit the rule.
var noRawAuthzChecksPackages = []string{
	"github.com/speakeasy-api/gram/server/internal/environments",
	"github.com/speakeasy-api/gram/server/internal/remotesessions",
}

// noRawAuthzCheckConstructors are the authz constructors a typed ref
// replaces. Constructors for kinds without a generated ref (auth clients,
// risk policies, inference) stay allowed.
var noRawAuthzCheckConstructors = []string{
	"AssistantCheck",
	"ChatReadCheck",
	"ChatWriteCheck",
	"EnvironmentCheck",
	"HooksIngestCheck",
	"MCPCheck",
	"MCPToolCallCheck",
}

type noRawAuthzChecksSettings struct {
	Disabled bool `json:"disabled"`

	// Packages overrides the migrated package prefixes, for tests whose
	// packages cannot carry the production import paths.
	Packages []string `json:"packages"`
}

func newNoRawAuthzChecksAnalyzer(rule noRawAuthzChecksSettings) *analysis.Analyzer {
	migrated := noRawAuthzChecksPackages
	if len(rule.Packages) > 0 {
		migrated = rule.Packages
	}

	return &analysis.Analyzer{
		Name:     noRawAuthzChecksAnalyzer,
		Doc:      noRawAuthzChecksDefaultMessage,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			if !packageMatches(pass.Pkg.Path(), migrated) {
				return nil, nil
			}
			ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
			ins.WithStack([]ast.Node{(*ast.CompositeLit)(nil), (*ast.CallExpr)(nil)}, func(node ast.Node, push bool, stack []ast.Node) bool {
				if !push {
					return true
				}
				// Tests may build checks to assert on them.
				if strings.HasSuffix(pass.Fset.File(node.Pos()).Name(), "_test.go") {
					return true
				}
				switch n := node.(type) {
				case *ast.CompositeLit:
					// The zero value returned beside an error names
					// nothing and is refused if evaluated.
					if isAuthzCheckType(pass.TypesInfo.TypeOf(n)) && !zeroCheckBesideError(n, stack) {
						pass.ReportRangef(n, "replace the authz.Check literal: %s", noRawAuthzChecksDefaultMessage)
					}
				case *ast.CallExpr:
					fn, ok := typeutil.Callee(pass.TypesInfo, n).(*types.Func)
					if !ok || fn.Pkg() == nil || fn.Pkg().Path() != authzPkgPath {
						return true
					}
					if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
						return true
					}
					if slices.Contains(noRawAuthzCheckConstructors, fn.Name()) {
						pass.ReportRangef(n, "replace authz.%s: %s", fn.Name(), noRawAuthzChecksDefaultMessage)
					}
				}
				return true
			})
			return nil, nil
		},
	}
}

// zeroCheckBesideError reports whether lit is an empty authz.Check{} returned
// next to an error that is not the literal nil, as in
// `return authz.Check{}, err`.
func zeroCheckBesideError(lit *ast.CompositeLit, stack []ast.Node) bool {
	if len(lit.Elts) > 0 || len(stack) < 2 {
		return false
	}
	ret, ok := stack[len(stack)-2].(*ast.ReturnStmt)
	if !ok || len(ret.Results) < 2 {
		return false
	}
	last := ret.Results[len(ret.Results)-1]
	if last == ast.Expr(lit) {
		return false
	}
	if id, ok := last.(*ast.Ident); ok && id.Name == "nil" {
		return false
	}
	return true
}

func packageMatches(path string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool {
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	})
}

func isAuthzCheckType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == "Check" && obj.Pkg() != nil && obj.Pkg().Path() == authzPkgPath
}
