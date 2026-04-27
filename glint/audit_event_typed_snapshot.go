package glint

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	auditEventTypedSnapshotAnalyzer = "auditeventtypedsnapshot"
	auditEventTypedSnapshotDoc      = "audit Log*Event SnapshotBefore/SnapshotAfter fields must use a typed Go type instead of any or interface{}"
	auditEventTypedSnapshotMessage  = "use typed struct field type, such as *types.Example, instead of arbitrary typed data"
)

// auditPkgPathSuffix gates the audit-event lint rules to the audit package.
// Suffix matching covers both the production import path and any analysistest
// testdata layout that ends in server/internal/audit.
const auditPkgPathSuffix = "server/internal/audit"

type auditEventTypedSnapshotSettings struct {
	Disabled bool `json:"disabled"`
}

func newAuditEventTypedSnapshotAnalyzer(rule auditEventTypedSnapshotSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: auditEventTypedSnapshotAnalyzer,
		Doc:  auditEventTypedSnapshotDoc,
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
							if !fieldHasSnapshotName(field) {
								continue
							}

							fieldType := pass.TypesInfo.TypeOf(field.Type)
							if !isEmptyInterface(fieldType) {
								continue
							}

							pass.ReportRangef(field, "%s", auditEventTypedSnapshotMessage)
						}
					}
				}
			}

			return nil, nil
		},
	}
}

// isLogEventTypeName reports whether a type name follows the audit Log*Event
// convention (Log prefix, Event suffix, with at least one character between).
func isLogEventTypeName(name string) bool {
	return len(name) > len("LogEvent") && strings.HasPrefix(name, "Log") && strings.HasSuffix(name, "Event")
}

// fieldHasSnapshotName reports whether any of the field's declared names
// contain SnapshotBefore or SnapshotAfter.
func fieldHasSnapshotName(field *ast.Field) bool {
	for _, name := range field.Names {
		if strings.Contains(name.Name, "SnapshotBefore") || strings.Contains(name.Name, "SnapshotAfter") {
			return true
		}
	}
	return false
}

// isEmptyInterface reports whether the type is the bare empty interface
// (`any` or `interface{}`). Named or anonymous interfaces with methods or
// embedded type sets are not flagged.
func isEmptyInterface(t types.Type) bool {
	if t == nil {
		return false
	}

	iface, ok := t.Underlying().(*types.Interface)
	if !ok {
		return false
	}

	return iface.Empty()
}
