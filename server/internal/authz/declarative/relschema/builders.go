package relschema

// Declarations are written with functional builders, as in the clean-slate
// spike:
//
//	NewType("mcp_server", Parent("project"), StoredKind("mcp"),
//		Grant("editor", StoredAs("mcp:write"), AnchoredAt("mcp_server", "project", "organization")),
//		Block("edit_blocked", StoredAs("mcp:blocked_write"), AnchoredAt("mcp_server", "project", "organization")),
//		Permission("edit", Union(Rel("editor"), AncestorRel("organization", "root")), ExcludedBy("edit_blocked")),
//	)
//
// Compile validates the result; the builders only assemble it.

type (
	TypeOption  func(*Type)
	GrantOption func(*GrantDef)
	PermOption  func(*PermDef)
)

// NewType declares a protected object type.
func NewType(name string, opts ...TypeOption) *Type {
	t := &Type{Name: name, Grants: map[string]*GrantDef{}, Perms: map[string]*PermDef{}, Attrs: map[string]string{}}
	for _, o := range opts {
		o(t)
	}
	return t
}

// Parent names the containing type.
func Parent(typ string) TypeOption { return func(t *Type) { t.Parent = typ } }

// Attr declares an attribute naming one related object of type typ.
func Attr(name, typ string) TypeOption { return func(t *Type) { t.Attrs[name] = typ } }

// StoredKind is the resource_kind stored rows use for grants on the type.
func StoredKind(kind string) TypeOption { return func(t *Type) { t.StoredKind = kind } }

// Grant declares a relation stored rows can confer on the type.
func Grant(name string, opts ...GrantOption) TypeOption {
	return func(t *Type) {
		g := &GrantDef{Name: name}
		for _, o := range opts {
			o(g)
		}
		t.Grants[name] = g
	}
}

// Block declares a relation that withdraws permissions.
func Block(name string, opts ...GrantOption) TypeOption {
	return func(t *Type) {
		Grant(name, opts...)(t)
		t.Grants[name].Block = true
	}
}

// Permission declares a computed permission.
func Permission(name string, expr Expr, opts ...PermOption) TypeOption {
	return func(t *Type) {
		p := &PermDef{Name: name, Expr: expr}
		for _, o := range opts {
			o(p)
		}
		t.Perms[name] = p
	}
}

// StoredAs maps a grant onto an existing stored scope.
func StoredAs(scope string) GrantOption { return func(g *GrantDef) { g.StoredScope = scope } }

// StoredKindAs overrides the type's resource_kind for one grant, e.g. root
// rows store "*".
func StoredKindAs(kind string) GrantOption { return func(g *GrantDef) { g.StoredKind = kind } }

// AnchoredAt lists the types a stored row may attach at: the type itself or
// an ancestor.
func AnchoredAt(types ...string) GrantOption {
	return func(g *GrantDef) { g.Anchors = append(g.Anchors, types...) }
}

// LegacyAnchoredAt lists anchors accepted only as legacy broad grants, which
// say so in every trace.
func LegacyAnchoredAt(types ...string) GrantOption {
	return func(g *GrantDef) { g.LegacyAnchors = append(g.LegacyAnchors, types...) }
}

// NarrowableBy lists ancestor types whose selector key may restrict a row
// anchored below them.
func NarrowableBy(types ...string) GrantOption {
	return func(g *GrantDef) { g.Narrowable = append(g.Narrowable, types...) }
}

// ConstrainedBy lists extra selector keys a row may carry, matched against
// request facts.
func ConstrainedBy(keys ...string) GrantOption {
	return func(g *GrantDef) { g.Constraints = append(g.Constraints, keys...) }
}

// Describe documents a grant for people choosing it.
func Describe(text string) GrantOption { return func(g *GrantDef) { g.Description = text } }

// ExcludedBy subtracts a block from the permission: a matching block beats
// every allow in it.
func ExcludedBy(block string) PermOption {
	return func(p *PermDef) { p.Expr = Minus(p.Expr, Rel(block)) }
}
