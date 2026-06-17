package glint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	noAnonymousDeferAnalyzer       = "noanonymousdefer"
	noAnonymousDeferDefaultMessage = "avoid anonymous deferred functions"
)

type noAnonymousDeferSettings struct {
	Disabled bool   `json:"disabled"`
	Message  string `json:"message"`
}

func newNoAnonymousDeferAnalyzer(rule noAnonymousDeferSettings) *analysis.Analyzer {
	message := noAnonymousDeferDefaultMessage
	if rule.Message != "" {
		message += ": " + rule.Message
	}

	return &analysis.Analyzer{
		Name:     noAnonymousDeferAnalyzer,
		Doc:      noAnonymousDeferDefaultMessage,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

			ins.Preorder([]ast.Node{(*ast.DeferStmt)(nil)}, func(node ast.Node) {
				deferStmt := node.(*ast.DeferStmt)

				if _, ok := deferStmt.Call.Fun.(*ast.FuncLit); !ok {
					return
				}

				pass.ReportRangef(deferStmt, "%s", message)
			})

			return nil, nil
		},
	}
}
