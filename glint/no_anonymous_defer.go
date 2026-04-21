package glint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
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
		Name: noAnonymousDeferAnalyzer,
		Doc:  noAnonymousDeferDefaultMessage,
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					deferStmt, ok := node.(*ast.DeferStmt)
					if !ok {
						return true
					}

					if _, ok := deferStmt.Call.Fun.(*ast.FuncLit); !ok {
						return true
					}

					pass.ReportRangef(deferStmt, "%s", message)

					return true
				})
			}

			return nil, nil
		},
	}
}
