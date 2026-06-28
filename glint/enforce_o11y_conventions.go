package glint

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	enforceO11yConventionsAnalyzer       = "enforceo11yconventions"
	enforceO11yConventionsDefaultMessage = "avoid direct slog attribute constructors"
)

var bannedSlogFuncs = []string{
	"Any",
	"Bool",
	"Duration",
	"Float64",
	"Group",
	"GroupAttrs",
	"Int",
	"Int64",
	"String",
	"Time",
	"Uint64",
}

type enforceO11yConventionsSettings struct {
	Disabled bool   `json:"disabled"`
	Message  string `json:"message"`
}

func newEnforceO11yConventionsAnalyzer(rule enforceO11yConventionsSettings) *analysis.Analyzer {
	message := enforceO11yConventionsDefaultMessage
	if rule.Message != "" {
		message += ": " + rule.Message
	}

	return &analysis.Analyzer{
		Name:     enforceO11yConventionsAnalyzer,
		Doc:      enforceO11yConventionsDefaultMessage,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

			ins.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(node ast.Node) {
				callExpr := node.(*ast.CallExpr)

				selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
				if !ok {
					return
				}

				called, ok := pass.TypesInfo.Uses[selectorExpr.Sel].(*types.Func)
				if !ok || called.Pkg() == nil {
					return
				}

				if called.Signature().Recv() != nil {
					return
				}

				if called.Pkg().Path() != "log/slog" || !slices.Contains(bannedSlogFuncs, called.Name()) {
					return
				}

				pass.ReportRangef(callExpr, "%s", message)
			})

			return nil, nil
		},
	}
}
