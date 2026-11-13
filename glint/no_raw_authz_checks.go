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
// authz.Project(p).Environments().Create(), ...). A hand-built authz.Check
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
			ins.Preorder([]ast.Node{(*ast.CompositeLit)(nil), (*ast.CallExpr)(nil)}, func(node ast.Node) {
				// Tests may build checks to assert on them.
				if strings.HasSuffix(pass.Fset.File(node.Pos()).Name(), "_test.go") {
					return
				}
				switch n := node.(type) {
				case *ast.CompositeLit:
					if isAuthzCheckType(pass.TypesInfo.TypeOf(n)) {
						pass.ReportRangef(n, "replace the authz.Check literal: %s", noRawAuthzChecksDefaultMessage)
					}
				case *ast.CallExpr:
					fn, ok := typeutil.Callee(pass.TypesInfo, n).(*types.Func)
					if !ok || fn.Pkg() == nil || fn.Pkg().Path() != authzPkgPath {
						return
					}
					if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
						return
					}
					if slices.Contains(noRawAuthzCheckConstructors, fn.Name()) {
						pass.ReportRangef(n, "replace authz.%s: %s", fn.Name(), noRawAuthzChecksDefaultMessage)
					}
				}
			})
			return nil, nil
		},
	}
}

func packageMatches(path string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool {
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	})
}

func isAuthzCheckType(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == "Check" && obj.Pkg() != nil && obj.Pkg().Path() == authzPkgPath
}
