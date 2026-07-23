package glint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"

	"golang.org/x/tools/go/analysis"

	"github.com/speakeasy-api/gram/glint/imports"
)

const (
	noBareSqlstateAnalyzer       = "nobaresqlstate"
	noBareSqlstateDefaultMessage = "compare pgconn.PgError.Code against a github.com/jackc/pgerrcode constant instead of a bare SQLSTATE string literal"

	pgconnPkgPath             = "github.com/jackc/pgx/v5/pgconn"
	pgErrorTypeName           = "PgError"
	pgErrorCodeFieldName      = "Code"
	pgerrcodePkgPath          = "github.com/jackc/pgerrcode"
	pgerrcodeDefaultLocalName = "pgerrcode"
)

// sqlstateConstNames maps SQLSTATE codes to their github.com/jackc/pgerrcode
// constant names so the suggested fix can name the code. It is a curated set,
// not the full SQLSTATE table: the ones that realistically appear in this
// codebase plus a few common cousins. Detection does not depend on it — any
// string literal compared against pgconn.PgError.Code is reported — but a code
// absent here is reported without an autofix, since we can't name its constant.
var sqlstateConstNames = map[string]string{
	"21000": "CardinalityViolation",
	"23502": "NotNullViolation",
	"23503": "ForeignKeyViolation",
	"23505": "UniqueViolation",
	"23514": "CheckViolation",
	"40001": "SerializationFailure",
	"40P01": "DeadlockDetected",
}

type noBareSqlstateSettings struct {
	Disabled bool `json:"disabled"`
}

func newNoBareSqlstateAnalyzer(_ noBareSqlstateSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: noBareSqlstateAnalyzer,
		Doc:  noBareSqlstateDefaultMessage,
		// Manual per-file walk rather than the shared inspector, for the same
		// reason as no_sql_err_no_rows: the fix adds the pgerrcode import once
		// per file, and that file-scoped edit has to ride along with every
		// occurrence's SuggestedFix. The shared inspector flattens the file
		// boundary the import edit needs, so adopting it would mean re-bucketing
		// nodes back by file with no traversal saved.
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				inspectFileForBareSqlstate(pass, file)
			}
			return nil, nil
		},
	}
}

// inspectFileForBareSqlstate walks file once, collecting comparisons of
// pgconn.PgError.Code against a string literal. It then reports one diagnostic
// per occurrence, attaching a SuggestedFix that swaps the literal for the
// pgerrcode constant (and adds the import when needed) for codes it can name.
func inspectFileForBareSqlstate(pass *analysis.Pass, file *ast.File) {
	type occurrence struct {
		lit       *ast.BasicLit
		constName string // "" when the code has no known pgerrcode constant
	}

	var occurrences []occurrence

	ast.Inspect(file, func(node ast.Node) bool {
		bin, ok := node.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		if bin.Op != token.EQL && bin.Op != token.NEQ {
			return true
		}

		lit, value, ok := bareSqlstateComparison(pass, bin)
		if !ok {
			return true
		}

		occurrences = append(occurrences, occurrence{lit: lit, constName: sqlstateConstNames[value]})
		return true
	})

	if len(occurrences) == 0 {
		return
	}

	localName, imported := imports.LocalName(file, pgerrcodePkgPath, pgerrcodeDefaultLocalName)

	fixable := false
	for _, occ := range occurrences {
		if occ.constName != "" {
			fixable = true
			break
		}
	}

	// importEdit is attached to every fixable occurrence's SuggestedFix, not
	// just the first: analysistest's three-way merge dedupes identical edits
	// when --fix applies them together, whereas splitting them risks an editor
	// quick-fix branch adding the import for one occurrence while siblings go
	// unfixed.
	var importEdits []analysis.TextEdit
	if fixable && !imported {
		if e, ok := imports.Add(file, pgerrcodePkgPath); ok {
			importEdits = append(importEdits, e)
		}
	}

	for _, occ := range occurrences {
		diag := analysis.Diagnostic{
			Pos:      occ.lit.Pos(),
			End:      occ.lit.End(),
			Category: noBareSqlstateAnalyzer,
			Message:  noBareSqlstateDefaultMessage,
		}

		if occ.constName != "" {
			replacement := []byte(localName + "." + occ.constName)
			edits := make([]analysis.TextEdit, 0, 1+len(importEdits))
			edits = append(edits, analysis.TextEdit{Pos: occ.lit.Pos(), End: occ.lit.End(), NewText: replacement})
			edits = append(edits, importEdits...)
			diag.SuggestedFixes = []analysis.SuggestedFix{{
				Message:   fmt.Sprintf("replace %s with %s.%s", occ.lit.Value, localName, occ.constName),
				TextEdits: edits,
			}}
		}

		pass.Report(diag)
	}
}

// bareSqlstateComparison reports whether bin compares pgconn.PgError.Code
// against a string literal, in either operand order, and returns the literal
// node and its unquoted value.
func bareSqlstateComparison(pass *analysis.Pass, bin *ast.BinaryExpr) (*ast.BasicLit, string, bool) {
	if isPgErrorCodeField(pass, bin.X) {
		if lit, value, ok := stringLiteral(bin.Y); ok {
			return lit, value, true
		}
	}
	if isPgErrorCodeField(pass, bin.Y) {
		if lit, value, ok := stringLiteral(bin.X); ok {
			return lit, value, true
		}
	}
	return nil, "", false
}

// isPgErrorCodeField reports whether expr selects the Code field of
// github.com/jackc/pgx/v5/pgconn.PgError, resolved through type information so
// it survives aliased imports and pointer vs value receivers.
func isPgErrorCodeField(pass *analysis.Pass, expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	selection, ok := pass.TypesInfo.Selections[sel]
	if !ok || selection.Kind() != types.FieldVal {
		return false
	}

	field := selection.Obj()
	if field == nil || field.Name() != pgErrorCodeFieldName {
		return false
	}

	recv := selection.Recv()
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}

	named, ok := recv.(*types.Named)
	if !ok {
		return false
	}

	obj := named.Obj()
	return obj != nil && obj.Pkg() != nil &&
		obj.Pkg().Path() == pgconnPkgPath && obj.Name() == pgErrorTypeName
}

// stringLiteral returns the node and unquoted value when expr is a string
// literal.
func stringLiteral(expr ast.Expr) (*ast.BasicLit, string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return nil, "", false
	}

	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return nil, "", false
	}

	return lit, value, true
}
