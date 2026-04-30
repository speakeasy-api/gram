// Package imports provides reusable helpers for emitting analysis.TextEdit
// values that add or remove imports from a Go source file. They are intended
// for use in glint analyzers' SuggestedFixes.
package imports

import (
	"go/ast"
	"go/token"
	"strconv"

	"golang.org/x/tools/go/analysis"
)

// LocalName looks up path in the file's imports and returns the name to use
// when referencing path's exported identifiers. Aliased imports return the
// alias; unaliased imports return defaultName. Blank ("_") and dot (".")
// imports are treated as not present so callers can add an explicit import
// rather than rely on a side-effect import. The second return value reports
// whether path was found as a regular import; when false the returned name
// is defaultName so callers can use it directly after pairing the returned
// edit from Add.
func LocalName(file *ast.File, path, defaultName string) (string, bool) {
	for _, imp := range file.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil || importPath != path {
			continue
		}
		if imp.Name == nil {
			return defaultName, true
		}
		switch imp.Name.Name {
		case "", "_", ".":
			continue
		default:
			return imp.Name.Name, true
		}
	}
	return defaultName, false
}

// Add emits a TextEdit that inserts a new import line into the first grouped
// import block found in file. Returns false if the file has no grouped import
// block (e.g. only single-line imports), in which case the caller should skip
// the import edit and let a follow-up tool such as goimports add the import.
//
// The new line is appended at the end of the block; callers that need the
// import block to remain canonically sorted should rely on a follow-up
// formatter such as gofmt or goimports.
func Add(file *ast.File, path string) (analysis.TextEdit, bool) {
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT || !gd.Lparen.IsValid() {
			continue
		}
		return analysis.TextEdit{
			Pos:     gd.Rparen,
			End:     gd.Rparen,
			NewText: []byte("\t" + strconv.Quote(path) + "\n"),
		}, true
	}
	return analysis.TextEdit{}, false
}

// Remove emits a TextEdit that deletes the entire line containing the import
// spec for path, including its trailing newline. Returns false if the import
// is not present in the file.
func Remove(fset *token.FileSet, file *ast.File, path string) (analysis.TextEdit, bool) {
	for _, imp := range file.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil || importPath != path {
			continue
		}
		tokFile := fset.File(imp.Pos())
		if tokFile == nil {
			return analysis.TextEdit{}, false
		}
		line := tokFile.Line(imp.Pos())
		lineStart := tokFile.LineStart(line)
		var lineEnd token.Pos
		if line < tokFile.LineCount() {
			lineEnd = tokFile.LineStart(line + 1)
		} else {
			lineEnd = token.Pos(tokFile.Base() + tokFile.Size())
		}
		return analysis.TextEdit{
			Pos:     lineStart,
			End:     lineEnd,
			NewText: nil,
		}, true
	}
	return analysis.TextEdit{}, false
}
