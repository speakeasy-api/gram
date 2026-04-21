package glint

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
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
		Name: enforceO11yConventionsAnalyzer,
		Doc:  enforceO11yConventionsDefaultMessage,
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					callExpr, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}

					selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}

					called, ok := pass.TypesInfo.Uses[selectorExpr.Sel].(*types.Func)
					if !ok || called.Pkg() == nil {
						return true
					}

					if called.Signature().Recv() != nil {
						return true
					}

					if called.Pkg().Path() != "log/slog" || !slices.Contains(bannedSlogFuncs, called.Name()) {
						return true
					}

					pass.ReportRangef(callExpr, "%s", message)

					return true
				})
			}

			return nil, nil
		},
	}
}
