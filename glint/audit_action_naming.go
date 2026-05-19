package glint

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	auditActionNamingAnalyzer = "auditactionnaming"
	auditActionNamingDoc      = "audit Action constants must use <namespace>:<verb> names with lowercase snake_case segments"
)

type auditActionNamingSettings struct {
	Disabled bool `json:"disabled"`
}

func newAuditActionNamingAnalyzer(rule auditActionNamingSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: auditActionNamingAnalyzer,
		Doc:  auditActionNamingDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if !strings.HasSuffix(pass.Pkg.Path(), auditPkgPathSuffix) {
				return nil, nil
			}

			for _, file := range pass.Files {
				for _, decl := range file.Decls {
					genDecl, ok := decl.(*ast.GenDecl)
					if !ok || genDecl.Tok != token.CONST {
						continue
					}

					for _, spec := range genDecl.Specs {
						valueSpec, ok := spec.(*ast.ValueSpec)
						if !ok {
							continue
						}

						for _, name := range valueSpec.Names {
							actionName, ok := auditActionConstName(pass.TypesInfo.Defs[name])
							if !ok {
								continue
							}

							if isAuditActionName(actionName) {
								continue
							}

							pass.ReportRangef(name, "name audit action %q as <namespace>:<verb> with lowercase snake_case segments", actionName)
						}
					}
				}
			}

			return nil, nil
		},
	}
}

func auditActionConstName(obj types.Object) (string, bool) {
	constantObj, ok := obj.(*types.Const)
	if !ok || constantObj.Val().Kind() != constant.String {
		return "", false
	}

	named, ok := constantObj.Type().(*types.Named)
	if !ok {
		return "", false
	}

	typeName := named.Obj()
	if typeName == nil || typeName.Pkg() == nil {
		return "", false
	}

	if typeName.Name() != "Action" || !strings.HasSuffix(typeName.Pkg().Path(), auditPkgPathSuffix) {
		return "", false
	}

	return constant.StringVal(constantObj.Val()), true
}

func isAuditActionName(name string) bool {
	parts := strings.Split(name, ":")
	if len(parts) != 2 {
		return false
	}

	return isAuditActionSegment(parts[0]) && isAuditActionSegment(parts[1])
}

func isAuditActionSegment(segment string) bool {
	if segment == "" {
		return false
	}

	for _, r := range segment {
		if r == '_' {
			continue
		}
		if r < 'a' || r > 'z' {
			return false
		}
	}

	return true
}
