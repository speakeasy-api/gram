package glint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

const (
	serviceHasAttachFuncAnalyzer = "servicehasattachfunc"
	serviceHasAttachFuncDoc      = "checks that packages defining an annotated service also declare a package-level Attach function"
)

type serviceHasAttachFuncSettings struct {
	Disabled bool `json:"disabled"`
}

func newServiceHasAttachFuncAnalyzer(rule serviceHasAttachFuncSettings) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: serviceHasAttachFuncAnalyzer,
		Doc:  serviceHasAttachFuncDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			annotated := findAnnotatedStructs(pass)
			if len(annotated) == 0 {
				return nil, nil
			}

			for _, s := range annotated {
				if !hasAttachFunc(pass, s.obj) {
					pass.ReportRangef(s.typeSpec, "%s embeds annotations.Service but package has no func Attach(..., *%s)",
						s.name, s.name)
				}
			}

			return nil, nil
		},
	}
}

func hasAttachFunc(pass *analysis.Pass, structObj *types.TypeName) bool {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if !ok || funcDecl.Recv != nil || funcDecl.Name.Name != "Attach" {
				continue
			}

			if funcDecl.Type.Params == nil {
				continue
			}

			for _, param := range funcDecl.Type.Params.List {
				paramType := pass.TypesInfo.TypeOf(param.Type)
				if paramType == nil {
					continue
				}

				ptr, ok := paramType.(*types.Pointer)
				if !ok {
					continue
				}

				named, ok := ptr.Elem().(*types.Named)
				if !ok {
					continue
				}

				if named.Obj() == structObj {
					return true
				}
			}
		}
	}

	return false
}
