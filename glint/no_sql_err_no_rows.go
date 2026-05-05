package glint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/speakeasy-api/gram/glint/imports"
)

const (
	noSqlErrNoRowsAnalyzer       = "nosqlerrnorows"
	noSqlErrNoRowsDefaultMessage = "use github.com/jackc/pgx/v5.ErrNoRows instead of database/sql.ErrNoRows"
	noSqlErrNoRowsFixMessage     = "replace database/sql.ErrNoRows with github.com/jackc/pgx/v5.ErrNoRows"

	sqlPkgPath          = "database/sql"
	sqlErrNoRowsName    = "ErrNoRows"
	pgxPkgPath          = "github.com/jackc/pgx/v5"
	pgxDefaultLocalName = "pgx"
)

type noSqlErrNoRowsSettings struct {
	Disabled bool `json:"disabled"`
}

func newNoSqlErrNoRowsAnalyzer(_ noSqlErrNoRowsSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: noSqlErrNoRowsAnalyzer,
		Doc:  noSqlErrNoRowsDefaultMessage,
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				inspectFileForSqlErrNoRows(pass, file)
			}
			return nil, nil
		},
	}
}

// inspectFileForSqlErrNoRows walks file once, collecting database/sql.ErrNoRows
// references and noting whether database/sql is used for any other symbol.
// It then reports one diagnostic per occurrence with a SuggestedFix that
// rewrites the selector to pgx.ErrNoRows and (when safe) updates imports.
func inspectFileForSqlErrNoRows(pass *analysis.Pass, file *ast.File) {
	var occurrences []*ast.SelectorExpr
	otherSqlUsage := false

	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.SelectorExpr:
			if isSqlErrNoRows(pass, n.Sel) {
				occurrences = append(occurrences, n)
				return false
			}
		case *ast.Ident:
			obj := pass.TypesInfo.Uses[n]
			if obj != nil && obj.Pkg() != nil &&
				obj.Pkg().Path() == sqlPkgPath && obj.Name() != sqlErrNoRowsName {
				otherSqlUsage = true
			}
		}
		return true
	})

	if len(occurrences) == 0 {
		return
	}

	pgxLocalName, pgxImported := imports.LocalName(file, pgxPkgPath, pgxDefaultLocalName)
	replacement := []byte(pgxLocalName + "." + sqlErrNoRowsName)

	// importEdits is intentionally attached to every diagnostic's SuggestedFix
	// rather than just the first. analysistest's three-way merge dedupes
	// identical edits when --fix applies them all together. Splitting them
	// across diagnostics would risk a different IDE quick-fix branch removing
	// the import while sibling occurrences remained unfixed.
	var importEdits []analysis.TextEdit
	if !pgxImported {
		if e, ok := imports.Add(file, pgxPkgPath); ok {
			importEdits = append(importEdits, e)
		}
	}
	if !otherSqlUsage {
		if e, ok := imports.Remove(pass.Fset, file, sqlPkgPath); ok {
			importEdits = append(importEdits, e)
		}
	}

	for _, occ := range occurrences {
		edits := make([]analysis.TextEdit, 0, 1+len(importEdits))
		edits = append(edits, analysis.TextEdit{Pos: occ.Pos(), End: occ.End(), NewText: replacement})
		edits = append(edits, importEdits...)

		pass.Report(analysis.Diagnostic{
			Pos:      occ.Pos(),
			End:      occ.End(),
			Category: noSqlErrNoRowsAnalyzer,
			Message:  noSqlErrNoRowsDefaultMessage,
			SuggestedFixes: []analysis.SuggestedFix{{
				Message:   noSqlErrNoRowsFixMessage,
				TextEdits: edits,
			}},
		})
	}
}

// isSqlErrNoRows reports whether ident resolves to the database/sql.ErrNoRows
// var via type information, so it works through aliased imports and avoids
// matching unrelated identifiers that happen to be named ErrNoRows.
func isSqlErrNoRows(pass *analysis.Pass, ident *ast.Ident) bool {
	obj, ok := pass.TypesInfo.Uses[ident].(*types.Var)
	if !ok {
		return false
	}
	if obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Path() == sqlPkgPath && obj.Name() == sqlErrNoRowsName
}
