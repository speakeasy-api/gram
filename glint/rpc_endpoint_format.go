package glint

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	rpcEndpointFormatAnalyzer       = "rpcendpointformat"
	rpcEndpointFormatDefaultMessage = "RPC endpoint paths must use the form /rpc/<namespace>.<verb>"

	rpcEndpointPathPrefix = "/rpc/"
	goaDSLPkgPath         = "goa.design/goa/v3/dsl"
)

// httpRouteFuncs are the goa DSL functions that declare an HTTP route with a
// path as their first argument.
var httpRouteFuncs = []string{
	"GET",
	"HEAD",
	"POST",
	"PUT",
	"DELETE",
	"CONNECT",
	"OPTIONS",
	"TRACE",
	"PATCH",
}

type rpcEndpointFormatSettings struct {
	Disabled bool `json:"disabled"`
}

func newRpcEndpointFormatAnalyzer(rule rpcEndpointFormatSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     rpcEndpointFormatAnalyzer,
		Doc:      rpcEndpointFormatDefaultMessage,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

			ins.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(node ast.Node) {
				callExpr := node.(*ast.CallExpr)

				// The goa DSL is dot-imported, so route declarations look like
				// GET("/rpc/..."). The callee is therefore a bare identifier.
				ident, ok := callExpr.Fun.(*ast.Ident)
				if !ok {
					return
				}

				called, ok := pass.TypesInfo.Uses[ident].(*types.Func)
				if !ok || called.Pkg() == nil {
					return
				}

				if called.Pkg().Path() != goaDSLPkgPath || !slices.Contains(httpRouteFuncs, called.Name()) {
					return
				}

				if len(callExpr.Args) == 0 {
					return
				}

				lit, ok := callExpr.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return
				}

				path, err := strconv.Unquote(lit.Value)
				if err != nil {
					return
				}

				// Only the RPC surface follows this convention.
				if !strings.HasPrefix(path, rpcEndpointPathPrefix) {
					return
				}

				if isValidRPCEndpointPath(path) {
					return
				}

				pass.ReportRangef(lit, "format RPC endpoint %q as /rpc/<namespace>.<verb> with lowercase-led alphanumeric segments", path)
			})

			return nil, nil
		},
	}
}

// isValidRPCEndpointPath reports whether path is exactly
// /rpc/<namespace>.<verb> with both segments being lowercase-led alphanumeric
// identifiers.
func isValidRPCEndpointPath(path string) bool {
	rest, ok := strings.CutPrefix(path, rpcEndpointPathPrefix)
	if !ok {
		return false
	}

	namespace, verb, ok := strings.Cut(rest, ".")
	if !ok {
		return false
	}

	return isRPCEndpointSegment(namespace) && isRPCEndpointSegment(verb)
}

// isRPCEndpointSegment reports whether segment starts with a lowercase letter
// and is otherwise composed only of ASCII letters and digits.
func isRPCEndpointSegment(segment string) bool {
	if segment == "" {
		return false
	}

	for i, r := range segment {
		switch {
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= 'A' && r <= 'Z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}

	return true
}
