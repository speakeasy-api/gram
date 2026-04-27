package glint

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	auditEventURNNamingAnalyzer = "auditeventurnnaming"
	auditEventURNNamingDoc      = "audit Log*Event subject identifier fields must use URN naming and a URN type instead of an Id/ID suffix"
	auditEventURNNamingMessage  = "use URN field naming and URN type instead of ID"
)

// auditEventURNNamingExemptFields are subject-identifier-shaped names that
// are exempt from URN naming because they are not subject identifiers.
// ProjectID and OrganizationID identify the audit row's scope, not its
// subject. Matching is exact: subject ids that happen to share a suffix
// (e.g. SourceProjectID) are not exempt.
var auditEventURNNamingExemptFields = map[string]struct{}{
	"ProjectID":      {},
	"OrganizationID": {},
}

type auditEventURNNamingSettings struct {
	Disabled bool `json:"disabled"`
}

func newAuditEventURNNamingAnalyzer(rule auditEventURNNamingSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: auditEventURNNamingAnalyzer,
		Doc:  auditEventURNNamingDoc,
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
							for _, name := range field.Names {
								if _, exempt := auditEventURNNamingExemptFields[name.Name]; exempt {
									continue
								}

								if !hasIDSuffix(name.Name) {
									continue
								}

								pass.ReportRangef(name, "%s", auditEventURNNamingMessage)
							}
						}
					}
				}
			}

			return nil, nil
		},
	}
}

// hasIDSuffix reports whether a field name ends in either Id or ID. Suffix
// matching avoids false positives on names that contain "Id" elsewhere
// (e.g. Identifier).
func hasIDSuffix(name string) bool {
	return strings.HasSuffix(name, "ID") || strings.HasSuffix(name, "Id")
}
