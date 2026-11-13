// Package schema is the declaration language for authorization types,
// grantable relations and computed permissions. A schema is written once in Go
// (see package model) and compiled at startup; everything else — evaluation,
// lookup, catalog and generated constants — derives from it.
package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Self names the object itself as a grant anchor.
const Self = "self"

type Type struct {
	Name         string
	Parent       string   // fixed containment chain: mcp_server → project → organization
	MemberOf     []string // object-side memberships resolved from org facts (user → group)
	Grants       map[string]*GrantDef
	Attrs        map[string]string // attribute name → target type, read from the loaded row
	Markers      map[string]string // marker name → description
	MarkerScopes map[string]string // marker name → stored scope (Option D)
	Perms        map[string]*PermDef

	chain []string // resolved ancestors, nearest first
}

type GrantDef struct {
	Name        string
	Anchors     []string // Self, an ancestor type, or a MemberOf type
	Conditions  []string
	Block       bool
	Delegable   bool
	Sensitive   bool
	Description string

	// StoredScope maps the grant onto an existing stored scope (Option D:
	// schema semantics over today's principal_grants rows).
	StoredScope string
	// SubjectAttrs makes the grant subject-scoped: its stored
	// subject_principal is matched against these attributes of the object
	// (and their memberships), e.g. whose activity a reviewer may read.
	SubjectAttrs []string
}

type PermDef struct {
	Name string
	Expr Expr
}

// Expr is a permission expression. The operator set is deliberately the
// subset OpenFGA also supports, so a schema can be exported as an oracle.
type Expr interface{ isExpr() }

type (
	RelExpr    struct{ Name string }       // a grantable relation, walking its anchors
	PermExpr   struct{ Name string }       // another permission on the same type
	UnionExpr  struct{ Items []Expr }      // any
	MinusExpr  struct{ Base, Sub Expr }    // base and not sub
	IsExpr     struct{ Attr string }       // the caller is the attribute's value
	ViaExpr    struct{ Attr, Perm string } // the permission on the attribute's object
	MarkedExpr struct{ Marker string }     // the object carries a marker
)

func (RelExpr) isExpr()    {}
func (PermExpr) isExpr()   {}
func (UnionExpr) isExpr()  {}
func (MinusExpr) isExpr()  {}
func (IsExpr) isExpr()     {}
func (ViaExpr) isExpr()    {}
func (MarkedExpr) isExpr() {}

func Rel(name string) Expr       { return RelExpr{Name: name} }
func Perm(name string) Expr      { return PermExpr{Name: name} }
func Union(items ...Expr) Expr   { return UnionExpr{Items: items} }
func Minus(base, sub Expr) Expr  { return MinusExpr{Base: base, Sub: sub} }
func Is(attr string) Expr        { return IsExpr{Attr: attr} }
func Via(attr, perm string) Expr { return ViaExpr{Attr: attr, Perm: perm} }
func Marked(marker string) Expr  { return MarkedExpr{Marker: marker} }
func ExcludedBy(block string) PermOption {
	return func(p *PermDef) { p.Expr = Minus(p.Expr, Rel(block)) }
}
func Describe(text string) GrantOption { return func(g *GrantDef) { g.Description = text } }
func Delegable() GrantOption           { return func(g *GrantDef) { g.Delegable = true } }
func Sensitive() GrantOption           { return func(g *GrantDef) { g.Sensitive = true } }
func Conditions(names ...string) GrantOption {
	return func(g *GrantDef) { g.Conditions = append(g.Conditions, names...) }
}
func AnchoredAt(types ...string) GrantOption {
	return func(g *GrantDef) { g.Anchors = append(g.Anchors, types...) }
}

type (
	TypeOption  func(*Type)
	GrantOption func(*GrantDef)
	PermOption  func(*PermDef)
)

func NewType(name string, opts ...TypeOption) *Type {
	t := &Type{Name: name, Grants: map[string]*GrantDef{}, Attrs: map[string]string{}, Markers: map[string]string{}, MarkerScopes: map[string]string{}, Perms: map[string]*PermDef{}}
	for _, o := range opts {
		o(t)
	}
	return t
}

func Parent(typ string) TypeOption        { return func(t *Type) { t.Parent = typ } }
func MemberOf(typ string) TypeOption      { return func(t *Type) { t.MemberOf = append(t.MemberOf, typ) } }
func Attr(name, typ string) TypeOption    { return func(t *Type) { t.Attrs[name] = typ } }
func Marker(name, desc string) TypeOption { return func(t *Type) { t.Markers[name] = desc } }

// MarkerAs stores a marker as a grant-shaped row with the given scope.
func MarkerAs(name, desc, scope string) TypeOption {
	return func(t *Type) { t.Markers[name] = desc; t.MarkerScopes[name] = scope }
}

// StoredAs maps a grant onto an existing stored scope.
func StoredAs(scope string) GrantOption { return func(g *GrantDef) { g.StoredScope = scope } }

// SubjectScoped limits a grant to objects whose attributes match its subject.
func SubjectScoped(attrs ...string) GrantOption {
	return func(g *GrantDef) { g.SubjectAttrs = append(g.SubjectAttrs, attrs...) }
}

func Grant(name string, opts ...GrantOption) TypeOption {
	return func(t *Type) {
		g := &GrantDef{Name: name}
		for _, o := range opts {
			o(g)
		}
		t.Grants[name] = g
	}
}

func Block(name string, opts ...GrantOption) TypeOption {
	return func(t *Type) {
		Grant(name, opts...)(t)
		t.Grants[name].Block = true
	}
}

func Permission(name string, expr Expr, opts ...PermOption) TypeOption {
	return func(t *Type) {
		p := &PermDef{Name: name, Expr: expr}
		for _, o := range opts {
			o(p)
		}
		t.Perms[name] = p
	}
}

// Tri is a three-valued truth used by conditions and evaluation.
type Tri uint8

const (
	False Tri = iota
	Unknown
	True
)

func Bool(b bool) Tri {
	if b {
		return True
	}
	return False
}

// Condition evaluates a stored condition's parameters against request facts.
type Condition struct {
	Name        string
	Eval        func(params json.RawMessage, reqCtx any) Tri
	Satisfiable func(params json.RawMessage) bool
	// Covers reports whether a grant with parent params includes everything a
	// grant with child params allows. Used by delegation containment.
	Covers func(parent, child json.RawMessage) bool
}

// NewCondition builds a typed condition. A request context of the wrong type
// yields Unknown, never True.
func NewCondition[P any, C any](name string, eval func(P, C) Tri) *Condition {
	decode := func(raw json.RawMessage) (P, bool) {
		var p P
		return p, json.Unmarshal(raw, &p) == nil
	}
	return &Condition{
		Name: name,
		Eval: func(raw json.RawMessage, reqCtx any) Tri {
			c, ok := reqCtx.(C)
			if !ok {
				return Unknown
			}
			p, ok := decode(raw)
			if !ok {
				return Unknown
			}
			return eval(p, c)
		},
		Satisfiable: func(raw json.RawMessage) bool { _, ok := decode(raw); return ok },
	}
}

type Schema struct {
	Types      map[string]*Type
	Conditions map[string]*Condition
	Hash       string // changes whenever the compiled meaning could change
}

// Compile validates references and resolves containment chains.
func Compile(types []*Type, conds []*Condition) (*Schema, error) {
	s := &Schema{Types: map[string]*Type{}, Conditions: map[string]*Condition{}}
	for _, c := range conds {
		s.Conditions[c.Name] = c
	}
	for _, t := range types {
		if _, dup := s.Types[t.Name]; dup {
			return nil, fmt.Errorf("duplicate type %q", t.Name)
		}
		s.Types[t.Name] = t
	}
	for _, t := range types {
		seen := map[string]bool{t.Name: true}
		for p := t.Parent; p != ""; p = s.Types[p].Parent {
			if _, ok := s.Types[p]; !ok {
				return nil, fmt.Errorf("%s: unknown parent %q", t.Name, p)
			}
			if seen[p] {
				return nil, fmt.Errorf("%s: containment cycle at %q", t.Name, p)
			}
			seen[p] = true
			t.chain = append(t.chain, p)
		}
		for _, g := range t.Grants {
			for _, a := range g.Anchors {
				if a != Self && !slices.Contains(t.chain, a) && !slices.Contains(t.MemberOf, a) {
					return nil, fmt.Errorf("%s.%s: anchor %q is not an ancestor or membership", t.Name, g.Name, a)
				}
			}
			for _, c := range g.Conditions {
				if _, ok := s.Conditions[c]; !ok {
					return nil, fmt.Errorf("%s.%s: unknown condition %q", t.Name, g.Name, c)
				}
			}
		}
		for _, p := range t.Perms {
			if err := s.validate(t, p.Expr); err != nil {
				return nil, fmt.Errorf("%s.%s: %w", t.Name, p.Name, err)
			}
		}
	}
	s.Hash = s.hash()
	return s, nil
}

func (s *Schema) validate(t *Type, e Expr) error {
	switch x := e.(type) {
	case RelExpr:
		if _, ok := t.Grants[x.Name]; !ok {
			return fmt.Errorf("unknown relation %q", x.Name)
		}
	case PermExpr:
		if _, ok := t.Perms[x.Name]; !ok {
			return fmt.Errorf("unknown permission %q", x.Name)
		}
	case UnionExpr:
		for _, i := range x.Items {
			if err := s.validate(t, i); err != nil {
				return err
			}
		}
	case MinusExpr:
		if err := s.validate(t, x.Base); err != nil {
			return err
		}
		return s.validate(t, x.Sub)
	case IsExpr:
		if _, ok := t.Attrs[x.Attr]; !ok {
			return fmt.Errorf("unknown attribute %q", x.Attr)
		}
	case ViaExpr:
		target, ok := t.Attrs[x.Attr]
		if !ok && x.Attr != t.Parent {
			return fmt.Errorf("unknown attribute %q", x.Attr)
		}
		if !ok {
			target = t.Parent
		}
		if _, ok := s.Types[target].Perms[x.Perm]; !ok {
			return fmt.Errorf("%s has no permission %q", target, x.Perm)
		}
	case MarkedExpr:
		if _, ok := t.Markers[x.Marker]; !ok {
			return fmt.Errorf("unknown marker %q", x.Marker)
		}
	default:
		return fmt.Errorf("unknown expression %T", e)
	}
	return nil
}

// Chain returns a type's ancestors, nearest first.
func (t *Type) Chain() []string { return t.chain }

// StoredRelation is the relation name a grant uses when anchored at anchorType.
// At the object itself it is the bare grant name; above it, "<type>.<grant>".
func StoredRelation(objType, grant, anchorType string) string {
	if anchorType == Self || anchorType == objType {
		return grant
	}
	return objType + "." + grant
}

// Markers lists every marker relation, for loading org facts.
func (s *Schema) Markers() []string {
	var out []string
	for _, t := range s.Types {
		for m := range t.Markers {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func (s *Schema) hash() string {
	var b strings.Builder
	names := make([]string, 0, len(s.Types))
	for n := range s.Types {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t := s.Types[n]
		fmt.Fprintf(&b, "%s<%s|%v|%v|%v;", n, t.Parent, t.MemberOf, sortedKeys(t.Attrs), sortedKeys(t.Markers))
		for _, g := range sortedKeys(t.Grants) {
			fmt.Fprintf(&b, "%+v;", *t.Grants[g])
		}
		for _, p := range sortedKeys(t.Perms) {
			fmt.Fprintf(&b, "%s=%#v;", p, t.Perms[p].Expr)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
