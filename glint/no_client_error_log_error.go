package glint

import (
	"fmt"
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	noClientErrorLogErrorAnalyzer       = "noclienterrorlogerror"
	noClientErrorLogErrorDefaultMessage = "client-fault (4xx) oops error %q is logged at error level via .LogError; chain .LogWarn or .LogInfo instead so the OpenTelemetry span is not marked errored"

	oopsPkgPath            = "github.com/speakeasy-api/gram/server/internal/oops"
	oopsShareableErrorName = "ShareableError"
	oopsLogErrorName       = "LogError"
)

// clientErrorCodes is the set of oops.Code constant names this analyzer treats
// as client faults (4xx) eligible for opt-in enforcement. It is keyed by the Go
// constant identifier (e.g. CodeUnauthorized) to match how call sites and the
// Codes setting reference codes, and is the authoritative universe the Codes
// setting is validated against.
//
// It deliberately excludes codes that map to a 4xx status but are not client
// faults: CodeInvariantViolation (422, but flagged as a server fault) stays at
// error level, and CodeCanceled (499) is never authored directly (LogError's
// own cancellation handling already demotes it). Server-fault codes
// (CodeUnexpected, CodeGatewayError, CodeNotImplemented) are 5xx and out of
// scope.
var clientErrorCodes = map[string]struct{}{
	"CodeBadRequest":          {}, // 400
	"CodeUnauthorized":        {}, // 401
	"CodeInsufficientCredits": {}, // 402
	"CodeForbidden":           {}, // 403
	"CodeNotFound":            {}, // 404
	"CodeMethodNotAllowed":    {}, // 405
	"CodeConflict":            {}, // 409
	"CodeRequestTooLarge":     {}, // 413
	"CodeUnsupportedMedia":    {}, // 415
	"CodeInvalid":             {}, // 422 (validation, not a server fault)
	"CodeRateLimitExceeded":   {}, // 429
}

type noClientErrorLogErrorSettings struct {
	Disabled bool `json:"disabled"`

	// Codes is the opt-in allowlist of oops.Code constant names to enforce (e.g.
	// "CodeUnauthorized", "CodeForbidden"). An empty list reports nothing, which
	// lets the rule be wired in with no effect until codes are opted in one at a
	// time. This list setting is a deliberate exception to the package's
	// Disabled-only convention, motivated by the phased rollout across the
	// existing call sites.
	Codes []string `json:"codes"`
}

func newNoClientErrorLogErrorAnalyzer(rule noClientErrorLogErrorSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: noClientErrorLogErrorAnalyzer,
		// Doc intentionally omits the %q placeholder used in per-site diagnostics.
		Doc:      "flag opt-in client-fault (4xx) oops codes logged at error level via .LogError; only direct oops.E/oops.C chains with constant code args are analyzed",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			// Validate the opt-in list against the known client-fault codes so a
			// typo or a non-4xx code surfaces as a configuration error rather than
			// silently enforcing nothing.
			enabled, err := clientErrorCodeSet(rule.Codes)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", noClientErrorLogErrorAnalyzer, err)
			}

			if len(enabled) == 0 {
				return nil, nil
			}

			ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

			ins.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(node ast.Node) {
				logCall := node.(*ast.CallExpr)

				// Match a `.LogError(...)` method call whose receiver type is
				// *oops.ShareableError. The receiver type is the load-bearing
				// guard, so an unrelated LogError method elsewhere is not matched.
				sel, ok := logCall.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != oopsLogErrorName {
					return
				}
				if !isShareableErrorMethod(pass.TypesInfo.Uses[sel.Sel], oopsLogErrorName) {
					return
				}

				// The receiver expression must be a direct oops.E(...) / oops.C(...)
				// call so the authored code constant is visible. Errors stored in a
				// variable before logging are out of scope (accepted false
				// negatives) since the code cannot be resolved here.
				codeArg, ok := oopsConstructorCodeArg(pass, sel.X)
				if !ok {
					return
				}

				// Resolve the code argument to an oops.Code constant name (e.g.
				// "CodeUnauthorized"). A non-constant code (a variable holding the
				// code) yields no name and is skipped.
				code, ok := oopsCodeConstName(pass, codeArg)
				if !ok {
					return
				}

				if _, ok := enabled[code]; !ok {
					return
				}

				pass.ReportRangef(sel.Sel, noClientErrorLogErrorDefaultMessage, code)
			})

			return nil, nil
		},
	}
}

// clientErrorCodeSet validates the opt-in codes against the known client-fault
// codes and returns them as a set. An unknown or non-client-fault code is a
// configuration error.
func clientErrorCodeSet(codes []string) (map[string]struct{}, error) {
	enabled := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		if _, ok := clientErrorCodes[code]; !ok {
			return nil, fmt.Errorf("unknown or non-client-fault oops code %q in codes setting", code)
		}
		enabled[code] = struct{}{}
	}
	return enabled, nil
}

// isShareableErrorMethod reports whether obj is the named method on
// *oops.ShareableError, resolved via type information so it is robust against
// import aliases.
func isShareableErrorMethod(obj types.Object, name string) bool {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Name() != name {
		return false
	}

	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}

	recv := sig.Recv().Type()
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}

	named, ok := recv.(*types.Named)
	if !ok {
		return false
	}

	return named.Obj().Name() == oopsShareableErrorName &&
		named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == oopsPkgPath
}

// oopsConstructorCodeArg reports the code argument of a direct oops.E(...) or
// oops.C(...) call expression. Both constructors take the oops.Code as their
// first argument.
func oopsConstructorCodeArg(pass *analysis.Pass, expr ast.Expr) (ast.Expr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return nil, false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}

	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != oopsPkgPath {
		return nil, false
	}
	if fn.Signature().Recv() != nil {
		return nil, false
	}
	if !slices.Contains([]string{"E", "C"}, fn.Name()) {
		return nil, false
	}

	return call.Args[0], true
}

// oopsCodeConstName returns the identifier name of an oops.Code constant
// referenced by expr (e.g. "CodeUnauthorized" for oops.CodeUnauthorized),
// resolved via type information so it is robust against import aliases. It
// reports false for anything that is not a direct reference to an exported
// constant declared in the oops package, including a variable holding a code or
// a constant from another package.
func oopsCodeConstName(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	var ident *ast.Ident
	switch e := expr.(type) {
	case *ast.Ident:
		ident = e
	case *ast.SelectorExpr:
		ident = e.Sel
	default:
		return "", false
	}

	c, ok := pass.TypesInfo.Uses[ident].(*types.Const)
	if !ok || c.Pkg() == nil || c.Pkg().Path() != oopsPkgPath {
		return "", false
	}

	return c.Name(), true
}
