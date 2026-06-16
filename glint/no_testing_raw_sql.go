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
	noTestingRawSqlAnalyzer       = "notestingrawsql"
	noTestingRawSqlDefaultMessage = "use SQLc-generated methods from the relevant package's queries.sql (or testenv/testrepo for fixtures genuinely shared across packages)"

	pgxPackagePath     = "github.com/jackc/pgx/v5"
	pgxpoolPackagePath = "github.com/jackc/pgx/v5/pgxpool"
)

var noTestingRawSqlMethods = map[string]bool{
	"Begin":     true,
	"BeginTx":   true,
	"CopyFrom":  true,
	"Exec":      true,
	"Query":     true,
	"QueryRow":  true,
	"SendBatch": true,
}

type noTestingRawSqlSettings struct {
	Disabled bool `json:"disabled"`
}

func newNoTestingRawSqlAnalyzer(_ noTestingRawSqlSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     noTestingRawSqlAnalyzer,
		Doc:      noTestingRawSqlDefaultMessage,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

			ins.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(node ast.Node) {
				callExpr := node.(*ast.CallExpr)

				// This rule only applies to test files. The shared inspector
				// walks every file in the package, so filter by filename per
				// node rather than per file.
				if !strings.HasSuffix(pass.Fset.File(callExpr.Pos()).Name(), "_test.go") {
					return
				}

				selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
				if !ok {
					return
				}

				if !noTestingRawSqlMethods[selectorExpr.Sel.Name] {
					return
				}

				fn, ok := pass.TypesInfo.Uses[selectorExpr.Sel].(*types.Func)
				if !ok {
					return
				}

				sig, ok := fn.Type().(*types.Signature)
				if !ok || sig.Recv() == nil {
					return
				}

				if !isPgxReceiver(sig.Recv().Type()) {
					return
				}

				pass.ReportRangef(callExpr, "%s", noTestingRawSqlDefaultMessage)
			})

			return nil, nil
		},
	}
}

// isPgxReceiver reports whether t resolves to a named type defined in
// github.com/jackc/pgx/v5 or its pgxpool subpackage. The pointer is unwrapped
// so *pgx.Conn and *pgxpool.Pool match alongside the pgx.Tx / pgx.Querier
// interface receivers.
func isPgxReceiver(t types.Type) bool {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}

	named, ok := t.(*types.Named)
	if !ok {
		return false
	}

	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}

	path := obj.Pkg().Path()
	return path == pgxPackagePath || path == pgxpoolPackagePath
}
