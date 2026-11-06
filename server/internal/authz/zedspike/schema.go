package zedspike

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"

	core "github.com/authzed/spicedb/pkg/proto/core/v1"
	"github.com/authzed/spicedb/pkg/schemadsl/compiler"
	"github.com/authzed/spicedb/pkg/schemadsl/input"
)

//go:embed rbac.zed
var schemaSource string

// SchemaSource returns the embedded schema text.
func SchemaSource() string { return schemaSource }

// CompileSchema parses rbac.zed with SpiceDB's schema compiler. It does not
// run SpiceDB's type-system validation, which needs the development
// datastore; the schema test runs it.
func CompileSchema() (*compiler.CompiledSchema, error) {
	compiled, err := compiler.Compile(
		compiler.InputSchema{Source: input.Source("rbac.zed"), SchemaString: schemaSource},
		compiler.AllowUnprefixedObjectType(),
		compiler.DisallowExpirationFlag(),
		compiler.DisallowImportFlag(),
	)
	if err != nil {
		return nil, fmt.Errorf("compile rbac.zed: %w", err)
	}
	return compiled, nil
}

// LoadRegistry compiles the schema, walks the compiled definitions into a
// Registry and checks that the schema and the Go tables agree. An error is a
// programming error in rbac.zed or the tables and must stop startup.
func LoadRegistry() (*Registry, error) {
	compiled, err := CompileSchema()
	if err != nil {
		return nil, err
	}
	reg := &Registry{Types: map[string]*TypeDef{}, Caveats: map[string]*CaveatDef{}}
	for _, caveat := range compiled.CaveatDefinitions {
		reg.Caveats[caveat.GetName()] = caveatDef(caveat)
	}
	var errs []error
	for _, ns := range compiled.ObjectDefinitions {
		def, err := typeDef(ns)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		reg.Types[def.Name] = def
	}
	errs = append(errs, checkTables(reg)...)
	if len(errs) > 0 {
		return nil, fmt.Errorf("rbac.zed does not match the Go tables: %w", errors.Join(errs...))
	}
	return reg, nil
}

func typeDef(ns *core.NamespaceDefinition) (*TypeDef, error) {
	def := &TypeDef{Name: ns.GetName(), Relations: map[string]*RelationDef{}, Permissions: map[string]*PermissionDef{}}
	for _, rel := range ns.GetRelation() {
		if rewrite := rel.GetUsersetRewrite(); rewrite != nil {
			expr, err := rewriteExpr(rewrite)
			if err != nil {
				return nil, fmt.Errorf("%s#%s: %w", def.Name, rel.GetName(), err)
			}
			def.Permissions[rel.GetName()] = &PermissionDef{
				Name:     rel.GetName(),
				Internal: isInternalPermission(rel.GetName()),
				Expr:     expr,
			}
			continue
		}
		relation, err := relationDef(def.Name, rel)
		if err != nil {
			return nil, err
		}
		def.Relations[relation.Name] = relation
	}
	return def, nil
}

func relationDef(typeName string, rel *core.Relation) (*RelationDef, error) {
	out := &RelationDef{Name: rel.GetName()}
	for _, allowed := range rel.GetTypeInformation().GetAllowedDirectRelations() {
		subject := SubjectType{
			Type:     allowed.GetNamespace(),
			Relation: allowed.GetRelation(),
			Wildcard: allowed.GetPublicWildcard() != nil,
			Caveat:   allowed.GetRequiredCaveat().GetCaveatName(),
		}
		if subject.Relation == "..." {
			subject.Relation = ""
		}
		out.Subjects = append(out.Subjects, subject)
	}
	switch {
	case typeName == "role" && out.Name == "member":
		out.Kind = RelationMembership
	case Facts[RelationKey{typeName, out.Name}] != "":
		out.Kind = RelationFact
	case len(out.Subjects) == 1 && out.Subjects[0].Type == out.Name:
		// an edge is named after the type it points at: relation project: project
		out.Kind = RelationEdge
	default:
		kind, scope, err := scopeForRelation(out.Name)
		if err != nil {
			return nil, fmt.Errorf("%s#%s: %w", typeName, out.Name, err)
		}
		out.Kind, out.Scope = kind, scope
	}
	return out, nil
}

// scopeForRelation applies the naming rule: scope with ":" as "_", with an
// optional _direct or _own suffix.
func scopeForRelation(name string) (RelationKind, Scope, error) {
	if name == "root" {
		return RelationGrant, ScopeRoot, nil
	}
	kind := RelationGrant
	base := name
	switch {
	case strings.HasSuffix(name, "_direct"):
		kind, base = RelationDirect, strings.TrimSuffix(name, "_direct")
	case strings.HasSuffix(name, "_own"):
		kind, base = RelationOwnBlock, strings.TrimSuffix(name, "_own")
	}
	for scope := range ScopeCatalogue {
		if strings.Replace(string(scope), ":", "_", 1) != base {
			continue
		}
		blocked := ScopeCatalogue[scope].Block
		switch {
		case kind == RelationGrant && blocked:
			kind = RelationBlock
		case kind == RelationDirect && blocked:
			return 0, "", fmt.Errorf("a block has no _direct form")
		case kind == RelationOwnBlock && !blocked:
			return 0, "", fmt.Errorf("only blocks have an _own form")
		}
		return kind, scope, nil
	}
	return 0, "", fmt.Errorf("no scope in ScopeCatalogue is named %q", base)
}

func isInternalPermission(name string) bool {
	return strings.HasPrefix(name, "has_") || strings.HasPrefix(name, "blocked_")
}

func rewriteExpr(rewrite *core.UsersetRewrite) (Expr, error) {
	switch op := rewrite.GetRewriteOperation().(type) {
	case *core.UsersetRewrite_Union:
		children, err := childExprs(op.Union.GetChild())
		return Expr{Union: children}, err
	case *core.UsersetRewrite_Intersection:
		children, err := childExprs(op.Intersection.GetChild())
		return Expr{Intersect: children}, err
	case *core.UsersetRewrite_Exclusion:
		children, err := childExprs(op.Exclusion.GetChild())
		if err != nil {
			return Expr{}, err
		}
		if len(children) != 2 {
			return Expr{}, fmt.Errorf("exclusion with %d operands", len(children))
		}
		return Expr{Exclude: &Exclusion{Base: children[0], Subtracted: children[1]}}, nil
	default:
		return Expr{}, fmt.Errorf("unsupported rewrite %T", op)
	}
}

func childExprs(children []*core.SetOperation_Child) ([]Expr, error) {
	out := make([]Expr, 0, len(children))
	for _, child := range children {
		switch c := child.GetChildType().(type) {
		case *core.SetOperation_Child_ComputedUserset:
			out = append(out, Expr{Ref: c.ComputedUserset.GetRelation()})
		case *core.SetOperation_Child_TupleToUserset:
			out = append(out, Expr{Arrow: &Arrow{
				Edge:   c.TupleToUserset.GetTupleset().GetRelation(),
				Target: c.TupleToUserset.GetComputedUserset().GetRelation(),
			}})
		case *core.SetOperation_Child_UsersetRewrite:
			nested, err := rewriteExpr(c.UsersetRewrite)
			if err != nil {
				return nil, err
			}
			out = append(out, nested)
		default:
			// _this, nil, self and .any()/.all() arrows are valid SpiceDB but
			// outside the subset this schema uses.
			return nil, fmt.Errorf("unsupported expression %T", c)
		}
	}
	return out, nil
}

func caveatDef(caveat *core.CaveatDefinition) *CaveatDef {
	def := &CaveatDef{Name: caveat.GetName()}
	for name := range caveat.GetParameterTypes() {
		if strings.HasPrefix(name, "granted_") {
			def.GrantedParams = append(def.GrantedParams, name)
		} else {
			def.CheckParams = append(def.CheckParams, name)
		}
	}
	slices.Sort(def.GrantedParams)
	slices.Sort(def.CheckParams)
	return def
}
