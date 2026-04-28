package glint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

const (
	annotationsPkgPath  = "github.com/speakeasy-api/gram/glint/annotations"
	annotationsTypeName = "Service"
)

// annotatedStruct represents a struct declaration that embeds annotations.Service[ImplOf, AuthBy].
type annotatedStruct struct {
	typeSpec   *ast.TypeSpec
	structType *ast.StructType
	name       string
	implOf     types.Type
	authBy     types.Type
	obj        *types.TypeName
}

// findAnnotatedStructs scans the package's files for structs embedding
// annotations.Service[ImplOf, AuthBy] and returns metadata about each one.
func findAnnotatedStructs(pass *analysis.Pass) []annotatedStruct {
	var annotated []annotatedStruct

	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			ts, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}

			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}

			for _, field := range st.Fields.List {
				if len(field.Names) > 0 {
					continue // not an embedded field
				}

				tv, ok := pass.TypesInfo.Types[field.Type]
				if !ok {
					continue
				}

				named, ok := tv.Type.(*types.Named)
				if !ok {
					continue
				}

				origin := named.Origin()
				if origin.Obj().Pkg() == nil || origin.Obj().Pkg().Path() != annotationsPkgPath || origin.Obj().Name() != annotationsTypeName {
					continue
				}

				targs := named.TypeArgs()
				if targs == nil || targs.Len() < 2 {
					continue
				}

				typeNameObj := pass.TypesInfo.Defs[ts.Name]
				if typeNameObj == nil {
					continue
				}

				tn, ok := typeNameObj.(*types.TypeName)
				if !ok {
					continue
				}

				annotated = append(annotated, annotatedStruct{
					typeSpec:   ts,
					structType: st,
					name:       ts.Name.Name,
					implOf:     targs.At(0),
					authBy:     targs.At(1),
					obj:        tn,
				})
			}

			return true
		})
	}

	return annotated
}

// interfaceAssertion represents a `var _ SomeInterface = (*SomeStruct)(nil)` declaration.
type interfaceAssertion struct {
	ifaceType types.Type
	structObj *types.TypeName
}

func collectInterfaceAssertions(pass *analysis.Pass) []interfaceAssertion {
	var assertions []interfaceAssertion

	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}

			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}

				// Must be `var _ X = ...`
				if len(vs.Names) != 1 || vs.Names[0].Name != "_" {
					continue
				}

				// Must have an explicit type on the LHS
				if vs.Type == nil {
					continue
				}

				// Must have exactly one value on the RHS
				if len(vs.Values) != 1 {
					continue
				}

				// Resolve the interface type from the LHS
				ifaceTV, ok := pass.TypesInfo.Types[vs.Type]
				if !ok {
					continue
				}

				// Resolve the struct pointer type from the RHS: (*T)(nil)
				structObj := extractConversionTarget(pass, vs.Values[0])
				if structObj == nil {
					continue
				}

				assertions = append(assertions, interfaceAssertion{
					ifaceType: ifaceTV.Type,
					structObj: structObj,
				})
			}
		}
	}

	return assertions
}

// extractConversionTarget extracts the TypeName from an expression of the form (*T)(nil).
func extractConversionTarget(pass *analysis.Pass, expr ast.Expr) *types.TypeName {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil
	}

	nilIdent, ok := call.Args[0].(*ast.Ident)
	if !ok || nilIdent.Name != "nil" {
		return nil
	}

	paren, ok := call.Fun.(*ast.ParenExpr)
	if !ok {
		return nil
	}

	star, ok := paren.X.(*ast.StarExpr)
	if !ok {
		return nil
	}

	tv, ok := pass.TypesInfo.Types[star.X]
	if !ok {
		return nil
	}

	named, ok := tv.Type.(*types.Named)
	if !ok {
		return nil
	}

	return named.Obj()
}

func hasAssertion(assertions []interfaceAssertion, ifaceType types.Type, structObj *types.TypeName) bool {
	for _, a := range assertions {
		if a.structObj == structObj && types.Identical(a.ifaceType, ifaceType) {
			return true
		}
	}
	return false
}
