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

				if !isPgxReceiver(sig.Recv().Type()) && !isPgxShapedInterfaceMethod(sig) {
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

// isPgxShapedInterfaceMethod reports whether sig is a method on an interface
// whose first result is pgconn.CommandTag, pgx.Rows, or pgx.Row, i.e. the Exec,
// Query, or QueryRow half of a pgx connection. This catches the DBTX interface
// every sqlc package generates in its db.go, calls promoted through structs
// that embed it, and any hand-written interface of the same shape, none of
// which are declared in a pgx package and so escape isPgxReceiver.
//
// The result type is the signal rather than the sqlc-generated file header
// (isSqlcGenerated) because it is visible in the type system without parsing
// source files and also covers non-generated wrappers. Keying on pgx result
// types keeps other drivers with the same method names out of scope, such as
// the ClickHouse driver whose Exec returns only error and whose Query and
// QueryRow return its own Rows and Row types. Concrete receivers are excluded
// so that only abstractions over a pgx connection match, not arbitrary types
// that happen to return pgx results.
func isPgxShapedInterfaceMethod(sig *types.Signature) bool {
	if !types.IsInterface(sig.Recv().Type()) {
		return false
	}

	if sig.Results().Len() == 0 {
		return false
	}

	named, ok := sig.Results().At(0).Type().(*types.Named)
	if !ok {
		return false
	}

	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}

	switch obj.Pkg().Path() {
	case pgconnPkgPath:
		return obj.Name() == "CommandTag"
	case pgxPackagePath:
		return obj.Name() == "Rows" || obj.Name() == "Row"
	default:
		return false
	}
}
