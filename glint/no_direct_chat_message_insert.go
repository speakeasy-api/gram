package glint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	noDirectChatMessageInsertAnalyzer = "nodirectchatmessageinsert"
	noDirectChatMessageInsertMessage  = "write chat messages through chat.ChatMessageWriter so risk observers run and encrypted transcripts are sealed in the same transaction"

	chatRepoPkgPath            = "github.com/speakeasy-api/gram/server/internal/chat/repo"
	chatPkgPath                = "github.com/speakeasy-api/gram/server/internal/chat"
	transcriptStoreRepoPkgPath = "github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
	transcriptStorePkgPath     = "github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	transcriptFixturesPkgPath  = "github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	riskRepoPkgPath            = "github.com/speakeasy-api/gram/server/internal/risk/repo"
	testenvRepoPkgPath         = "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// chatMessageWriteQueries maps each sqlc package to the generated queries
// that write chat_messages or its transcript tables, and the one package
// allowed to call them. An empty allowed package means test files only. A
// test in internal/chat/transcriptstore checks this list against every SQL
// file, so a new write query fails CI until it is listed here.
var chatMessageWriteQueries = map[string]struct {
	allowed string
	queries map[string]bool
}{
	chatRepoPkgPath: {
		allowed: chatPkgPath,
		queries: map[string]bool{
			"CreateChatMessage":              true,
			"UpsertCorrelatedChatMessage":    true,
			"CreateExternalChatMessage":      true,
			"CreateChatMessageReturningID":   true,
			"CreateChatMessageWithToolCalls": true,
			"SeedChatMessage":                true,
			"SeedChatMessageContent":         true,
			"SeedChatMessageWithSource":      true,
			"SeedChatTranscriptMessage":      true,
		},
	},
	transcriptStoreRepoPkgPath: {
		allowed: transcriptStorePkgPath,
		queries: map[string]bool{
			"WriteChatMessage":      true,
			"Capture":               true,
			"UpdateMessageMetadata": true,
			"DeleteToolCallIDs":     true,
			"InsertToolCallIDs":     true,
		},
	},
	transcriptFixturesPkgPath: {
		allowed: "",
		queries: map[string]bool{
			"CreateMessage":     true,
			"ChangeContent":     true,
			"ReplaceCiphertext": true,
			"ChangeNotes":       true,
			"RaceContent":       true,
			"RaceRawContent":    true,
			"SetJSONField":      true,
			"SetStorageError":   true,
			"InsertTranscript":  true,
			"SetToolCalls":      true,
			"MakeLegacyCopy":    true,
		},
	},
	riskRepoPkgPath: {
		allowed: "",
		queries: map[string]bool{
			"CreateChatMessageForTest":              true,
			"CreateChatMessageWithToolCallsForTest": true,
			"CreateJudgeWindowMessageForTest":       true,
		},
	},
	testenvRepoPkgPath: {
		allowed: "",
		queries: map[string]bool{
			"InsertChatMessage":                   true,
			"SeedCapturedAgentChatMessageFixture": true,
		},
	},
}

type noDirectChatMessageInsertSettings struct {
	Disabled bool `json:"disabled"`
}

// newNoDirectChatMessageInsertAnalyzer flags production code that calls a
// chat message write query outside the package that owns it. Test files may
// seed rows directly.
func newNoDirectChatMessageInsertAnalyzer(rule noDirectChatMessageInsertSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     noDirectChatMessageInsertAnalyzer,
		Doc:      noDirectChatMessageInsertMessage,
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

				guard, ok := chatMessageWriteQueries[called.Pkg().Path()]
				if !ok || !guard.queries[called.Name()] {
					return
				}

				if pass.Pkg.Path() == guard.allowed {
					return
				}

				if strings.HasSuffix(pass.Fset.File(callExpr.Pos()).Name(), "_test.go") {
					return
				}

				pass.ReportRangef(callExpr, "do not call %s directly; %s", called.Name(), noDirectChatMessageInsertMessage)
			})

			return nil, nil
		},
	}
}
