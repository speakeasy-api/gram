// Package relschema is a bounded declarative relationship schema over stored
// grant rows. A schema declares object types, their fixed containment chain,
// the grantable relations each type accepts (and which stored scope each one
// reads), and the permissions computed from them. Decode maps one stored row
// onto a relationship the schema understands, or fails; Evaluate answers one
// permission on one object without I/O.
//
// The package holds machinery only. Which permissions exist, what they imply
// and which blocks withdraw them are declared by the authz package.
package relschema

import (
	"fmt"
	"slices"
)

// Self names the object itself as a grant anchor.
const Self = "self"

// Type is one protected object type.
type Type struct {
	Name string
	// Parent is the containing type: mcp_server → project → organization.
	Parent string
	// StoredKind is the resource_kind stored rows use for grants on this type.
	StoredKind string
	Grants     map[string]*Grant
	Perms      map[string]*Perm

	chain []string // resolved ancestors, nearest first
}

// Grant is a relation a stored row can confer on a type.
type Grant struct {
	Name string
	// StoredScope is the principal_grants scope this relation reads.
	StoredScope string
	// StoredKind overrides the type's resource_kind for this relation, e.g.
	// root rows use "*".
	StoredKind string
	// Anchors lists where a row may attach: Self or an ancestor type.
	Anchors []string
	// LegacyAnchors are anchors accepted only as legacy broad grants. A
	// relationship decoded at one is marked Legacy and says so in its trace.
	LegacyAnchors []string
	// Narrowable lists ancestor types whose selector key may narrow a row
	// anchored below them, e.g. a server row restricted to its project.
	Narrowable []string
	// Constraints lists extra selector keys the row may carry. They restrict
	// the relation to requests that carry matching facts.
	Constraints []string
	// Block marks a relation that withdraws permissions rather than granting.
	Block bool
}

// Perm is a computed permission.
type Perm struct {
	Name string
	Expr Expr
}

// Expr is a permission expression.
type Expr interface{ isExpr() }

type (
	// RelExpr is a grant declared on this type, walking its anchors.
	RelExpr struct{ Name string }
	// AncestorRelExpr is a grant declared on an ancestor type and held on
	// that ancestor, e.g. organization-wide root.
	AncestorRelExpr struct{ Type, Name string }
	// UnionExpr allows when any item allows.
	UnionExpr struct{ Items []Expr }
	// MinusExpr allows when Base allows and Sub does not.
	MinusExpr struct{ Base, Sub Expr }
)

func (RelExpr) isExpr()         {}
func (AncestorRelExpr) isExpr() {}
func (UnionExpr) isExpr()       {}
func (MinusExpr) isExpr()       {}

func Rel(name string) Expr                    { return RelExpr{Name: name} }
func AncestorRel(typ, name string) Expr       { return AncestorRelExpr{Type: typ, Name: name} }
func Union(items ...Expr) Expr                { return UnionExpr{Items: items} }
func Minus(base, sub Expr) Expr               { return MinusExpr{Base: base, Sub: sub} }
func ExcludedBy(base Expr, block string) Expr { return Minus(base, Rel(block)) }

// Schema is a compiled, validated set of types.
type Schema struct {
	Types map[string]*Type
	// AncestorKeys maps a selector key to the ancestor type it names.
	AncestorKeys map[string]string
	// Root is the top of every containment chain, the tenant boundary.
	Root string

	byScope    map[string]scopeTarget
	outOfScope map[string]struct{}
}

type scopeTarget struct {
	typ   *Type
	grant *Grant
}

// Definition is everything Compile needs.
type Definition struct {
	Types        []*Type
	AncestorKeys map[string]string
	Root         string
	// KnownScopes is every scope the stored grants can hold. Each must be
	// either read by a declared grant or listed in OutOfSchema, so a new
	// scope cannot be silently skipped.
	KnownScopes []string
	OutOfSchema []string
}

// Compile validates references, resolves containment chains and checks that
// the scope partition is exhaustive.
func Compile(def Definition) (*Schema, error) {
	s := &Schema{
		Types:        map[string]*Type{},
		AncestorKeys: def.AncestorKeys,
		Root:         def.Root,
		byScope:      map[string]scopeTarget{},
		outOfScope:   map[string]struct{}{},
	}
	for _, t := range def.Types {
		if _, dup := s.Types[t.Name]; dup {
			return nil, fmt.Errorf("duplicate type %q", t.Name)
		}
		s.Types[t.Name] = t
	}
	if _, ok := s.Types[def.Root]; !ok {
		return nil, fmt.Errorf("unknown root type %q", def.Root)
	}
	for key, typ := range def.AncestorKeys {
		if _, ok := s.Types[typ]; !ok {
			return nil, fmt.Errorf("ancestor key %q names unknown type %q", key, typ)
		}
	}
	for _, t := range def.Types {
		t.chain = nil
		seen := map[string]bool{t.Name: true}
		for p := t.Parent; p != ""; {
			pt, ok := s.Types[p]
			if !ok {
				return nil, fmt.Errorf("%s: unknown parent %q", t.Name, p)
			}
			if seen[p] {
				return nil, fmt.Errorf("%s: containment cycle at %q", t.Name, p)
			}
			seen[p] = true
			t.chain = append(t.chain, p)
			p = pt.Parent
		}
		if t.Name != def.Root && (len(t.chain) == 0 || t.chain[len(t.chain)-1] != def.Root) {
			return nil, fmt.Errorf("%s: containment chain does not end at %q", t.Name, def.Root)
		}
		for _, g := range t.Grants {
			if err := s.compileGrant(t, g); err != nil {
				return nil, err
			}
		}
		for _, p := range t.Perms {
			if err := s.validate(t, p.Expr); err != nil {
				return nil, fmt.Errorf("%s.%s: %w", t.Name, p.Name, err)
			}
		}
	}
	for _, scope := range def.OutOfSchema {
		if _, mapped := s.byScope[scope]; mapped {
			return nil, fmt.Errorf("scope %q is both mapped and out of schema", scope)
		}
		s.outOfScope[scope] = struct{}{}
	}
	for _, scope := range def.KnownScopes {
		_, mapped := s.byScope[scope]
		_, out := s.outOfScope[scope]
		if !mapped && !out {
			return nil, fmt.Errorf("scope %q is neither mapped nor declared out of schema", scope)
		}
	}
	return s, nil
}

func (s *Schema) compileGrant(t *Type, g *Grant) error {
	if g.StoredScope == "" {
		return fmt.Errorf("%s.%s: no stored scope", t.Name, g.Name)
	}
	if prev, dup := s.byScope[g.StoredScope]; dup {
		return fmt.Errorf("scope %q mapped twice (%s.%s and %s.%s)", g.StoredScope, prev.typ.Name, prev.grant.Name, t.Name, g.Name)
	}
	if g.kind(t) == "" {
		return fmt.Errorf("%s.%s: no stored resource kind", t.Name, g.Name)
	}
	for _, a := range slices.Concat(g.Anchors, g.LegacyAnchors) {
		if !t.anchorable(a) {
			return fmt.Errorf("%s.%s: anchor %q is not the type or an ancestor", t.Name, g.Name, a)
		}
	}
	for _, n := range g.Narrowable {
		if !slices.Contains(t.chain, n) {
			return fmt.Errorf("%s.%s: narrowable %q is not an ancestor", t.Name, g.Name, n)
		}
		if s.ancestorKey(n) == "" {
			return fmt.Errorf("%s.%s: narrowable %q has no selector key", t.Name, g.Name, n)
		}
	}
	for _, c := range g.Constraints {
		if _, isAncestor := s.AncestorKeys[c]; isAncestor || c == keyResourceKind || c == keyResourceID {
			return fmt.Errorf("%s.%s: constraint %q is a reserved key", t.Name, g.Name, c)
		}
	}
	s.byScope[g.StoredScope] = scopeTarget{typ: t, grant: g}
	return nil
}

func (s *Schema) validate(t *Type, e Expr) error {
	switch x := e.(type) {
	case RelExpr:
		if _, ok := t.Grants[x.Name]; !ok {
			return fmt.Errorf("unknown relation %q", x.Name)
		}
	case AncestorRelExpr:
		if !slices.Contains(t.chain, x.Type) && x.Type != t.Name {
			return fmt.Errorf("%q is not an ancestor", x.Type)
		}
		if _, ok := s.Types[x.Type].Grants[x.Name]; !ok {
			return fmt.Errorf("%s has no relation %q", x.Type, x.Name)
		}
	case UnionExpr:
		for _, item := range x.Items {
			if err := s.validate(t, item); err != nil {
				return err
			}
		}
	case MinusExpr:
		if err := s.validate(t, x.Base); err != nil {
			return err
		}
		if err := s.validate(t, x.Sub); err != nil {
			return err
		}
		if rel, ok := x.Sub.(RelExpr); ok && !t.Grants[rel.Name].Block {
			return fmt.Errorf("exclusion %q is not a block", rel.Name)
		}
	default:
		return fmt.Errorf("unknown expression %T", e)
	}
	return nil
}

func (s *Schema) ancestorKey(typ string) string {
	for key, t := range s.AncestorKeys {
		if t == typ {
			return key
		}
	}
	return ""
}

func (t *Type) anchorable(anchor string) bool {
	return anchor == Self || anchor == t.Name || slices.Contains(t.chain, anchor)
}

func (g *Grant) kind(t *Type) string {
	if g.StoredKind != "" {
		return g.StoredKind
	}
	return t.StoredKind
}

// Chain returns a type's ancestors, nearest first.
func (t *Type) Chain() []string { return t.chain }
