package glint

// no-query-skip-async-insert-wait forbids turning off wait_for_async_insert in
// ClickHouse query settings. clickhouse-go sends query settings after the
// connection's, so a per-query 0 overrides the test connection's 1 and makes a
// test's read race the async insert queue — a race SYSTEM FLUSH ASYNC INSERT
// QUEUE cannot close, because the server's background flusher can dequeue a
// batch just before the flush looks. Fire-and-forget belongs on the server
// connection (server/cmd/gram/deps.go); queries set only async_insert=1.
//
// Any clickhouse.Settings value counts as query settings — a literal or an
// index assignment, wherever it is later passed — except the Settings field of
// clickhouse.Options, which configures the connection.

import (
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	noQuerySkipAsyncInsertWaitAnalyzer       = "noqueryskipasyncinsertwait"
	noQuerySkipAsyncInsertWaitDefaultMessage = "set wait_for_async_insert on the connection, not per query: production connections set 0 and the test connection sets 1 so tests read their own writes"
)

const (
	clickhousePkgPath      = "github.com/ClickHouse/clickhouse-go/v2"
	waitForAsyncInsertName = "wait_for_async_insert"
)

type noQuerySkipAsyncInsertWaitSettings struct {
	Disabled bool `json:"disabled"`
}

func newNoQuerySkipAsyncInsertWaitAnalyzer(_ noQuerySkipAsyncInsertWaitSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     noQuerySkipAsyncInsertWaitAnalyzer,
		Doc:      noQuerySkipAsyncInsertWaitDefaultMessage,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run:      runNoQuerySkipAsyncInsertWait,
	}
}

func runNoQuerySkipAsyncInsertWait(pass *analysis.Pass) (any, error) {
	ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	// Connection settings literals, recorded when their clickhouse.Options
	// literal or field assignment is visited; Preorder reaches the parent first.
	connection := make(map[*ast.CompositeLit]bool)

	ins.Preorder([]ast.Node{(*ast.CompositeLit)(nil), (*ast.AssignStmt)(nil)}, func(node ast.Node) {
		switch n := node.(type) {
		case *ast.CompositeLit:
			typ := pass.TypesInfo.TypeOf(n)
			if isClickhouseNamed(typ, "Options") {
				for _, elt := range n.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Settings" {
						if lit, ok := ast.Unparen(kv.Value).(*ast.CompositeLit); ok {
							connection[lit] = true
						}
					}
				}
				return
			}
			if !isClickhouseNamed(typ, "Settings") || connection[n] {
				return
			}
			for _, elt := range n.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if ok && isConstString(pass, kv.Key, waitForAsyncInsertName) && isConstZero(pass, kv.Value) {
					pass.ReportRangef(kv, "%s", noQuerySkipAsyncInsertWaitDefaultMessage)
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if isOptionsSettingsField(pass, lhs) && i < len(n.Rhs) {
					if lit, ok := ast.Unparen(n.Rhs[i]).(*ast.CompositeLit); ok {
						connection[lit] = true
					}
					continue
				}
				index, ok := lhs.(*ast.IndexExpr)
				if !ok || i >= len(n.Rhs) || len(n.Lhs) != len(n.Rhs) {
					continue
				}
				if !isClickhouseNamed(pass.TypesInfo.TypeOf(index.X), "Settings") || isOptionsSettingsField(pass, index.X) {
					continue
				}
				if isConstString(pass, index.Index, waitForAsyncInsertName) && isConstZero(pass, n.Rhs[i]) {
					pass.ReportRangef(n, "%s", noQuerySkipAsyncInsertWaitDefaultMessage)
				}
			}
		}
	})

	return nil, nil
}

// isClickhouseNamed reports whether typ is the named clickhouse-go type name.
func isClickhouseNamed(typ types.Type, name string) bool {
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == clickhousePkgPath && named.Obj().Name() == name
}

// isOptionsSettingsField reports whether expr selects the Settings field of a
// clickhouse.Options value, i.e. connection settings.
func isOptionsSettingsField(pass *analysis.Pass, expr ast.Expr) bool {
	sel, ok := ast.Unparen(expr).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Settings" {
		return false
	}
	selection, ok := pass.TypesInfo.Selections[sel]
	if !ok || selection.Kind() != types.FieldVal {
		return false
	}
	return isClickhouseNamed(selection.Recv(), "Options")
}

func isConstString(pass *analysis.Pass, expr ast.Expr, want string) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return false
	}
	return constant.StringVal(tv.Value) == want
}

// isConstZero reports whether expr is a constant that ClickHouse reads as an
// off setting: numeric zero, false, or their string spellings.
func isConstZero(pass *analysis.Pass, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil {
		return false
	}
	switch tv.Value.Kind() {
	case constant.Int, constant.Float:
		return constant.Sign(tv.Value) == 0
	case constant.Bool:
		return !constant.BoolVal(tv.Value)
	case constant.String:
		s := constant.StringVal(tv.Value)
		return s == "0" || s == "false"
	default:
		return false
	}
}
