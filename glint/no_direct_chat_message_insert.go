package glint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	noDirectChatMessageInsertAnalyzer = "nodirectchatmessageinsert"
	noDirectChatMessageInsertMessage  = "do not call CreateChatMessage directly; use chat.ChatMessageWriter.Write() or .WriteTurn() to ensure risk analysis observers are notified"

	chatRepoPkgPath = "github.com/speakeasy-api/gram/server/internal/chat/repo"
	chatPkgPath     = "github.com/speakeasy-api/gram/server/internal/chat"
)

type noDirectChatMessageInsertSettings struct {
	Disabled bool `json:"disabled"`
}

func newNoDirectChatMessageInsertAnalyzer(rule noDirectChatMessageInsertSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     noDirectChatMessageInsertAnalyzer,
		Doc:      noDirectChatMessageInsertMessage,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			// Allow calls from within the chat package itself.
			if pass.Pkg.Path() == chatPkgPath {
				return nil, nil
			}

			ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

			ins.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(node ast.Node) {
				callExpr := node.(*ast.CallExpr)

				selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
				if !ok {
					return
				}

				if selectorExpr.Sel.Name != "CreateChatMessage" {
					return
				}

				called, ok := pass.TypesInfo.Uses[selectorExpr.Sel].(*types.Func)
				if !ok || called.Pkg() == nil {
					return
				}

				if called.Pkg().Path() != chatRepoPkgPath {
					return
				}

				pass.ReportRangef(callExpr, "%s", noDirectChatMessageInsertMessage)
			})

			return nil, nil
		},
	}
}
