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
	"github.com/speakeasy-api/gram/server/internal/access",
	"github.com/speakeasy-api/gram/server/internal/platformmcp",
	"github.com/speakeasy-api/gram/server/internal/platformtools",
	"github.com/speakeasy-api/gram/server/internal/risk",
	"github.com/speakeasy-api/gram/server/internal/skills",
	"github.com/speakeasy-api/gram/server/internal/skillefficacy",
	"github.com/speakeasy-api/gram/server/internal/plugins",
	"github.com/speakeasy-api/gram/server/internal/usersessions",
	"github.com/speakeasy-api/gram/server/internal/organizations",
	"github.com/speakeasy-api/gram/server/internal/authclients",
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections",
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers",
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets",
	"github.com/speakeasy-api/gram/server/internal/externalcredentials",
	"github.com/speakeasy-api/gram/server/internal/externalkeys",
	"github.com/speakeasy-api/gram/server/internal/orgencryptionapi",
	"github.com/speakeasy-api/gram/server/internal/productfeatures",
	"github.com/speakeasy-api/gram/server/internal/networkingress",
	"github.com/speakeasy-api/gram/server/internal/customdomains",
	"github.com/speakeasy-api/gram/server/internal/identityapi",
	"github.com/speakeasy-api/gram/server/internal/cliauth",
	"github.com/speakeasy-api/gram/server/internal/auth",
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections",
	"github.com/speakeasy-api/gram/server/internal/oktaserversuggestions",
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections",
	"github.com/speakeasy-api/gram/server/internal/slackapp",
	"github.com/speakeasy-api/gram/server/internal/auditapi",
	"github.com/speakeasy-api/gram/server/internal/mcp",
	"github.com/speakeasy-api/gram/server/internal/remotemcp",
	"github.com/speakeasy-api/gram/server/internal/metamcp",
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp",
	"github.com/speakeasy-api/gram/server/internal/unproxiedmcp",
	"github.com/speakeasy-api/gram/server/internal/externalmcp",
	"github.com/speakeasy-api/gram/server/internal/shadowmcp",
	"github.com/speakeasy-api/gram/server/internal/mcpapproval",
	"github.com/speakeasy-api/gram/server/internal/mcpservers",
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints",
	"github.com/speakeasy-api/gram/server/internal/mcpregistry",
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata",
	"github.com/speakeasy-api/gram/server/internal/hiddenmcpservers",
	"github.com/speakeasy-api/gram/server/internal/toolsets",
	"github.com/speakeasy-api/gram/server/internal/tools",
	"github.com/speakeasy-api/gram/server/internal/deployments",
	"github.com/speakeasy-api/gram/server/internal/packages",
	"github.com/speakeasy-api/gram/server/internal/variations",
	"github.com/speakeasy-api/gram/server/internal/templates",
	"github.com/speakeasy-api/gram/server/internal/resources",
	"github.com/speakeasy-api/gram/server/internal/assets",
	"github.com/speakeasy-api/gram/server/internal/agent",
	"github.com/speakeasy-api/gram/server/internal/agentmanagement",
	"github.com/speakeasy-api/gram/server/internal/assistants",
	"github.com/speakeasy-api/gram/server/internal/assistantmemories",
	"github.com/speakeasy-api/gram/server/internal/assistantidentity",
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy",
	"github.com/speakeasy-api/gram/server/internal/chat",
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations",
	"github.com/speakeasy-api/gram/server/internal/hooks",
	"github.com/speakeasy-api/gram/server/internal/businessmemory",
	"github.com/speakeasy-api/gram/server/internal/telemetry",
	"github.com/speakeasy-api/gram/server/internal/usage",
	"github.com/speakeasy-api/gram/server/internal/spendrules",
	"github.com/speakeasy-api/gram/server/internal/dashboards",
	"github.com/speakeasy-api/gram/server/internal/widgets",
	"github.com/speakeasy-api/gram/server/internal/dataexports",
	"github.com/speakeasy-api/gram/server/internal/sigint",
	"github.com/speakeasy-api/gram/server/internal/triggers",
	"github.com/speakeasy-api/gram/server/internal/aiintegrations",
	"github.com/speakeasy-api/gram/server/internal/modelkeys",
	"github.com/speakeasy-api/gram/server/internal/litellm",
	"github.com/speakeasy-api/gram/server/internal/otel",
	"github.com/speakeasy-api/gram/server/internal/projects",
	"github.com/speakeasy-api/gram/server/internal/keys",
	"github.com/speakeasy-api/gram/server/cmd/gram",
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
