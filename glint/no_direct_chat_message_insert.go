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

// testsOnly marks a query that only test files may call.
const testsOnly = ""

// chatMessageWriteQueries maps each sqlc package to the generated queries
// that write chat_messages' protected fields or its transcript tables, and,
// per query, the one package allowed to call it. testsOnly means test files
// only. A test in internal/chat/transcriptstore checks this list against
// every SQL file, so a new write query fails CI until it is listed here.
var chatMessageWriteQueries = map[string]map[string]string{
	chatRepoPkgPath: {
		"CreateChatMessage":              chatPkgPath,
		"UpsertCorrelatedChatMessage":    chatPkgPath,
		"CreateExternalChatMessage":      chatPkgPath,
		"CreateChatMessageReturningID":   testsOnly,
		"CreateChatMessageWithToolCalls": testsOnly,
		"SeedChatMessage":                testsOnly,
		"SeedChatMessageContent":         testsOnly,
		"SeedChatMessageWithSource":      testsOnly,
		"SeedChatTranscriptMessage":      testsOnly,
	},
	transcriptStoreRepoPkgPath: {
		"WriteChatMessage":      transcriptStorePkgPath,
		"Capture":               transcriptStorePkgPath,
		"UpdateMessageMetadata": transcriptStorePkgPath,
		"DeleteToolCallIDs":     transcriptStorePkgPath,
		"InsertToolCallIDs":     transcriptStorePkgPath,
	},
	transcriptFixturesPkgPath: {
		"CreateMessage":     testsOnly,
		"ChangeContent":     testsOnly,
		"ReplaceCiphertext": testsOnly,
		"ChangeNotes":       testsOnly,
		"RaceContent":       testsOnly,
		"RaceRawContent":    testsOnly,
		"SetJSONField":      testsOnly,
		"SetStorageError":   testsOnly,
		"InsertTranscript":  testsOnly,
		"SetToolCalls":      testsOnly,
		"MakeLegacyCopy":    testsOnly,
	},
	riskRepoPkgPath: {
		"CreateChatMessageForTest":              testsOnly,
		"CreateChatMessageWithToolCallsForTest": testsOnly,
		"CreateJudgeWindowMessageForTest":       testsOnly,
	},
	testenvRepoPkgPath: {
		"InsertChatMessage":                   testsOnly,
		"SeedCapturedAgentChatMessageFixture": testsOnly,
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

				allowed, ok := chatMessageWriteQueries[called.Pkg().Path()][called.Name()]
				if !ok {
					return
				}

				if allowed != testsOnly && pass.Pkg.Path() == allowed {
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
