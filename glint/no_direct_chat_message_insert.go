package glint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

const (
	noDirectChatMessageInsertAnalyzer = "nodirectchatmessageinsert"
	noDirectChatMessageInsertMessage  = "do not call CreateChatMessage directly; use chat.ChatMessageWriter.Write() or .RunInTx() to ensure risk analysis observers are notified"

	chatRepoPkgPath = "github.com/speakeasy-api/gram/server/internal/chat/repo"
	chatPkgPath     = "github.com/speakeasy-api/gram/server/internal/chat"
)

type noDirectChatMessageInsertSettings struct {
	Disabled bool `json:"disabled"`
}

func newNoDirectChatMessageInsertAnalyzer(rule noDirectChatMessageInsertSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: noDirectChatMessageInsertAnalyzer,
		Doc:  noDirectChatMessageInsertMessage,
		Run: func(pass *analysis.Pass) (any, error) {
			// Allow calls from within the chat package itself.
			if pass.Pkg.Path() == chatPkgPath {
				return nil, nil
			}

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

					if selectorExpr.Sel.Name != "CreateChatMessage" {
						return true
					}

					called, ok := pass.TypesInfo.Uses[selectorExpr.Sel].(*types.Func)
					if !ok || called.Pkg() == nil {
						return true
					}

					if called.Pkg().Path() != chatRepoPkgPath {
						return true
					}

					pass.ReportRangef(callExpr, "%s", noDirectChatMessageInsertMessage)

					return true
				})
			}

			return nil, nil
		},
	}
}
