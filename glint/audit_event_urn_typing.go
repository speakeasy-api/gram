package glint

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	auditEventURNTypingAnalyzer = "auditeventurntyping"
	auditEventURNTypingDoc      = "audit Log*Event URN-named fields must use a type from server/internal/urn"
	auditEventURNTypingMessage  = "use server/internal/urn type"
)

// urnPkgPathSuffix matches both the production import path of the URN package
// and any analysistest layout that ends in server/internal/urn.
const urnPkgPathSuffix = "server/internal/urn"

type auditEventURNTypingSettings struct {
	Disabled bool `json:"disabled"`
}

func newAuditEventURNTypingAnalyzer(rule auditEventURNTypingSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: auditEventURNTypingAnalyzer,
		Doc:  auditEventURNTypingDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if !strings.HasSuffix(pass.Pkg.Path(), auditPkgPathSuffix) {
				return nil, nil
			}

			for _, file := range pass.Files {
				for _, decl := range file.Decls {
					genDecl, ok := decl.(*ast.GenDecl)
					if !ok || genDecl.Tok != token.TYPE {
						continue
					}

					for _, spec := range genDecl.Specs {
						typeSpec, ok := spec.(*ast.TypeSpec)
						if !ok {
							continue
						}

						if !isLogEventTypeName(typeSpec.Name.Name) {
							continue
						}

						structType, ok := typeSpec.Type.(*ast.StructType)
						if !ok {
							continue
						}

						for _, field := range structType.Fields.List {
							if !fieldHasURNName(field) {
								continue
							}

							if isURNTyped(pass.TypesInfo.TypeOf(field.Type)) {
								continue
							}

							pass.ReportRangef(field, "%s", auditEventURNTypingMessage)
						}
					}
				}
			}

			return nil, nil
		},
	}
}

// fieldHasURNName reports whether any of the field's declared names ends in
// either Urn or URN.
func fieldHasURNName(field *ast.Field) bool {
	for _, name := range field.Names {
		if strings.HasSuffix(name.Name, "URN") || strings.HasSuffix(name.Name, "Urn") {
			return true
		}
	}
	return false
}

// isURNTyped reports whether t is a named type defined in the URN package,
// drilling through a single level of pointer indirection. Slices, maps, and
// other composite carriers of URN types are intentionally not treated as
// URN-typed: audit Log*Event fields are expected to be plain values.
func isURNTyped(t types.Type) bool {
	if t == nil {
		return false
	}

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

	return strings.HasSuffix(obj.Pkg().Path(), urnPkgPathSuffix)
}
