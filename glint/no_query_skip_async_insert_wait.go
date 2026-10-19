package glint

// no-query-skip-async-insert-wait forbids turning off wait_for_async_insert in
// per-query ClickHouse settings. clickhouse-go sends query settings after the
// connection's, so a per-query 0 overrides the test connection's 1 and makes a
// test's read race the async insert queue — a race SYSTEM FLUSH ASYNC INSERT
// QUEUE cannot close, because the server's background flusher can dequeue a
// batch just before the flush looks. Fire-and-forget belongs on the server
// connection (server/cmd/gram/deps.go); queries set only async_insert=1.

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

const clickhousePkgPath = "github.com/ClickHouse/clickhouse-go/v2"

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

	ins.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(node ast.Node) {
		call := node.(*ast.CallExpr)
		if !isClickhouseWithSettings(pass, call) {
			return
		}
		for _, arg := range call.Args {
			lit, ok := ast.Unparen(arg).(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if isConstString(pass, kv.Key, "wait_for_async_insert") && isConstZero(pass, kv.Value) {
					pass.ReportRangef(kv, "%s", noQuerySkipAsyncInsertWaitDefaultMessage)
				}
			}
		}
	})

	return nil, nil
}

func isClickhouseWithSettings(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return false
	}
	return fn.Pkg().Path() == clickhousePkgPath && fn.Name() == "WithSettings"
}

func isConstString(pass *analysis.Pass, expr ast.Expr, want string) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return false
	}
	return constant.StringVal(tv.Value) == want
}

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
	default:
		return false
	}
}
