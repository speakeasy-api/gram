package glint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
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
		Name: noTestingRawSqlAnalyzer,
		Doc:  noTestingRawSqlDefaultMessage,
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				if !strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
					continue
				}

				ast.Inspect(file, func(node ast.Node) bool {
					callExpr, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}

					selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}

					if !noTestingRawSqlMethods[selectorExpr.Sel.Name] {
						return true
					}

					fn, ok := pass.TypesInfo.Uses[selectorExpr.Sel].(*types.Func)
					if !ok {
						return true
					}

					sig, ok := fn.Type().(*types.Signature)
					if !ok || sig.Recv() == nil {
						return true
					}

					if !isPgxReceiver(sig.Recv().Type()) {
						return true
					}

					pass.ReportRangef(callExpr, "%s", noTestingRawSqlDefaultMessage)
					return true
				})
			}

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
