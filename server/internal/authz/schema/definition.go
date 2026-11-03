// Package schema declares authorization resource types and compiles them into
// an immutable registry.
//
// Each resource type is declared once with NewType. A declaration binds local
// relation names to existing stored scopes (Grant, Block), records the facts a
// service loads for the resource (Attr, Derived, Guard), and composes the
// operations callers check (Permission). Compile validates every declaration
// and returns a Registry whose projections (scope metadata, legacy check
// satisfaction, containment closure, catalogue sub-scopes and the agent
// runtime lifecycle) are derived from the declarations rather than maintained
// beside them.
//
// A declaration reads like this:
//
//	schema.NewType("mcp_server", schema.Parent("project"), schema.StoredKind("mcp"),
//	    schema.SelectorKeys("project_id", "tool", "disposition"),
//	    schema.WrittenBy("access"),
//	    schema.Grant("viewer", schema.StoredAs("mcp:read"),
//	        schema.AnchoredAt("organization", "project", schema.Self),
//	        schema.Conditions("mcp_tools"),
//	        schema.Describe("Inspect server configuration")),
//	    schema.Block("read_blocked", schema.StoredAs("mcp:blocked_read"),
//	        schema.Conditions("mcp_tools")),
//	    schema.Permission("read", schema.Rel("viewer"),
//	        schema.Checks("mcp:read"), schema.ExcludedBy("read_blocked")),
//	)
//
// The package only declares and compiles. It evaluates nothing, performs no
// I/O and imports no service or authz code, so authz can depend on it.
// Parent and AnchoredAt describe containment and where grants may be stored;
// neither verifies ancestry nor makes a parent's permissions apply to a child.
package schema

import "slices"

// Self names the declared type itself in AnchoredAt.
const Self = "$self"

// TypeDecl is the declaration of one resource type, built by NewType and
// consumed by Compile.
type TypeDecl struct {
	name         string
	parent       string
	storedKind   string
	selectorKeys []string
	writtenBy    string
	attrs        []attrDecl
	factOrder    []string
	derived      []derivedDecl
	guards       []guardDecl
	relations    []relationDecl
	permissions  []permissionDecl
	alternatives []alternativesDecl
}

// Name returns the declared type name.
func (t TypeDecl) Name() string { return t.name }

type attrDecl struct {
	name     string
	factType string
}

type derivedDecl struct {
	name       string
	projection Projection
}

type guardDecl struct {
	name string
	cond Cond
}

type relationKind string

const (
	relationGrant relationKind = "grant"
	relationBlock relationKind = "block"
)

type relationDecl struct {
	kind          relationKind
	name          string
	storedAs      string
	anchors       []string
	conditions    []string
	selectorKeys  []string
	description   string
	internal      bool
	sensitive     bool
	strict        bool
	writtenBy     string
	roles         []string
	runtimeSince  int
	subjectScoped bool
}

type permissionDecl struct {
	name        string
	expr        Expr
	excludedBy  []string
	checks      string
	description string
}

type alternativesDecl struct {
	name    string
	options []PermissionRef
	veto    bool
}

// PermissionRef names a permission on a declared type.
type PermissionRef struct {
	// Type is the declared resource type.
	Type string

	// Permission is the permission declared on Type.
	Permission string
}

// TypeOption configures a TypeDecl.
type TypeOption interface {
	applyType(t *TypeDecl)
}

// RelationOption configures a Grant or Block.
type RelationOption interface {
	applyRelation(r *relationDecl)
}

// PermissionOption configures a Permission.
type PermissionOption interface {
	applyPermission(p *permissionDecl)
}

// AlternativesOption configures named Alternatives.
type AlternativesOption interface {
	applyAlternatives(a *alternativesDecl)
}

type typeOptionFunc func(t *TypeDecl)

func (f typeOptionFunc) applyType(t *TypeDecl) { f(t) }

type relationOptionFunc func(r *relationDecl)

func (f relationOptionFunc) applyRelation(r *relationDecl) { f(r) }

type permissionOptionFunc func(p *permissionDecl)

func (f permissionOptionFunc) applyPermission(p *permissionDecl) { f(p) }

// NewType declares a resource type. The name is also the stored selector
// resource kind unless StoredKind says otherwise.
func NewType(name string, opts ...TypeOption) TypeDecl {
	t := TypeDecl{
		name:         name,
		parent:       "",
		storedKind:   "",
		selectorKeys: nil,
		writtenBy:    "",
		attrs:        nil,
		factOrder:    nil,
		derived:      nil,
		guards:       nil,
		relations:    nil,
		permissions:  nil,
		alternatives: nil,
	}
	for _, opt := range opts {
		opt.applyType(&t)
	}
	return t
}

// Parent declares the type that contains this one. It records containment
// only: it neither verifies that a loaded resource belongs to the parent nor
// grants the parent's permissions on this type.
func Parent(typeName string) TypeOption {
	return typeOptionFunc(func(t *TypeDecl) { t.parent = typeName })
}

// StoredKind sets the selector resource_kind stored grants use for this type,
// when it differs from the type name.
func StoredKind(kind string) TypeOption {
	return typeOptionFunc(func(t *TypeDecl) { t.storedKind = kind })
}

// Attr declares a fact services load for the resource. factType is one of
// "string", "nullable_string", "bool", "nullable_timestamp", "user", or the
// name of another declared type, which makes the fact a reference Via can
// follow.
func Attr(name, factType string) TypeOption {
	return typeOptionFunc(func(t *TypeDecl) {
		t.attrs = append(t.attrs, attrDecl{name: name, factType: factType})
		t.factOrder = append(t.factOrder, name)
	})
}

// Derived declares a fact computed from other facts without I/O.
func Derived(name string, projection Projection) TypeOption {
	return typeOptionFunc(func(t *TypeDecl) {
		t.derived = append(t.derived, derivedDecl{name: name, projection: projection})
		t.factOrder = append(t.factOrder, name)
	})
}

// Guard declares a named condition that When can depend on.
func Guard(name string, cond Cond) TypeOption {
	return typeOptionFunc(func(t *TypeDecl) {
		t.guards = append(t.guards, guardDecl{name: name, cond: cond})
	})
}

// Grant binds a local relation to an existing stored allow scope.
func Grant(name string, opts ...RelationOption) TypeOption {
	return relation(relationGrant, name, opts)
}

// Block binds a local relation to an existing stored blocking scope
// (<resource>:blocked_<action>). Blocks are internal, match selectors
// strictly, and apply only through ExcludedBy.
func Block(name string, opts ...RelationOption) TypeOption {
	return relation(relationBlock, name, opts)
}

func relation(kind relationKind, name string, opts []RelationOption) TypeOption {
	opts = slices.Clone(opts)
	return typeOptionFunc(func(t *TypeDecl) {
		r := relationDecl{
			kind:          kind,
			name:          name,
			storedAs:      "",
			anchors:       nil,
			conditions:    nil,
			selectorKeys:  nil,
			description:   "",
			internal:      kind == relationBlock,
			sensitive:     false,
			strict:        kind == relationBlock,
			writtenBy:     "",
			roles:         nil,
			runtimeSince:  0,
			subjectScoped: false,
		}
		for _, opt := range opts {
			opt.applyRelation(&r)
		}
		t.relations = append(t.relations, r)
	})
}

// Permission declares an operation callers check, composed from relations,
// facts and other permissions.
func Permission(name string, expr Expr, opts ...PermissionOption) TypeOption {
	opts = slices.Clone(opts)
	return typeOptionFunc(func(t *TypeDecl) {
		p := permissionDecl{name: name, expr: expr, excludedBy: nil, checks: "", description: ""}
		for _, opt := range opts {
			opt.applyPermission(&p)
		}
		t.permissions = append(t.permissions, p)
	})
}

// Alternatives declares a named list of complete permission checks. A caller
// passes when one alternative passes in every policy; it is not a Union, so
// different policies cannot each satisfy a different alternative.
func Alternatives(name string, opts ...AlternativesOption) TypeOption {
	opts = slices.Clone(opts)
	return typeOptionFunc(func(t *TypeDecl) {
		a := alternativesDecl{name: name, options: nil, veto: false}
		for _, opt := range opts {
			opt.applyAlternatives(&a)
		}
		t.alternatives = append(t.alternatives, a)
	})
}

type alternativeOption PermissionRef

func (o alternativeOption) applyAlternatives(a *alternativesDecl) {
	a.options = append(a.options, PermissionRef(o))
}

// Alternative adds one complete permission check to Alternatives.
func Alternative(typeName, permission string) AlternativesOption {
	return alternativeOption{Type: typeName, Permission: permission}
}

type vetoBlocksOption struct{}

func (vetoBlocksOption) applyAlternatives(a *alternativesDecl) { a.veto = true }

// VetoBlocks rejects the request when any alternative's ExcludedBy block
// matches in any policy, even if another alternative passes.
func VetoBlocks() AlternativesOption { return vetoBlocksOption{} }

// StoredAs names the existing stored scope a Grant or Block reads.
func StoredAs(scope string) RelationOption {
	return relationOptionFunc(func(r *relationDecl) { r.storedAs = scope })
}

// AnchoredAt lists where a grant may be stored: Self or a type on the Parent
// chain. A grant anchored at a parent applies to every resource of this type
// under it through selector matching; it does not verify that relationship.
func AnchoredAt(levels ...string) RelationOption {
	levels = slices.Clone(levels)
	return relationOptionFunc(func(r *relationDecl) { r.anchors = append(r.anchors, levels...) })
}

// Conditions attaches registered pure conditions that every path through the
// relation must satisfy, such as MCP tool and disposition limits.
func Conditions(names ...string) RelationOption {
	names = slices.Clone(names)
	return relationOptionFunc(func(r *relationDecl) { r.conditions = append(r.conditions, names...) })
}

// Internal hides a grant from the permission catalogue.
func Internal() RelationOption {
	return relationOptionFunc(func(r *relationDecl) { r.internal = true })
}

// Sensitive marks a grant whose holder can read sensitive content.
func Sensitive() RelationOption {
	return relationOptionFunc(func(r *relationDecl) { r.sensitive = true })
}

// StrictMatching makes a grant match selectors strictly: every key the grant
// constrains must appear in the check.
func StrictMatching() RelationOption {
	return relationOptionFunc(func(r *relationDecl) { r.strict = true })
}

// DefaultFor lists the system roles that hold the grant by default.
func DefaultFor(roles ...string) RelationOption {
	roles = slices.Clone(roles)
	return relationOptionFunc(func(r *relationDecl) { r.roles = append(r.roles, roles...) })
}

// RuntimeSafeSince records the agent runtime registry version from which the
// scope is safe to place in a delegated runtime policy. Without it the scope
// is active but never safe.
func RuntimeSafeSince(version int) RelationOption {
	return relationOptionFunc(func(r *relationDecl) {
		r.runtimeSince = version
	})
}

// SubjectScoped is reserved for audience-limited grants. This foundation does
// not support it, and Compile rejects any declaration that uses it.
func SubjectScoped(_ ...string) RelationOption {
	return relationOptionFunc(func(r *relationDecl) { r.subjectScoped = true })
}

// ExcludedBy applies block precedence to the permission: a matching block
// removes access unless a qualifying direct user grant restores the matching
// instance, and the principal's own blocks always win. When the permission
// Checks a scope, the first block is that scope's direct exclusion and the
// rest are the broader blocks that also exclude it.
func ExcludedBy(blocks ...string) PermissionOption {
	blocks = slices.Clone(blocks)
	return permissionOptionFunc(func(p *permissionDecl) { p.excludedBy = append(p.excludedBy, blocks...) })
}

// Checks names the stored scope whose existing authz.Check this permission
// answers. The permission must be grant-only, so a check routed to it keeps
// today's grant semantics.
func Checks(scope string) PermissionOption {
	return permissionOptionFunc(func(p *permissionDecl) { p.checks = scope })
}

type describeOption string

func (d describeOption) applyRelation(r *relationDecl)     { r.description = string(d) }
func (d describeOption) applyPermission(p *permissionDecl) { p.description = string(d) }

// DescribeOption sets catalogue text on a Grant or Permission.
type DescribeOption interface {
	RelationOption
	PermissionOption
}

// Describe sets the catalogue description of a Grant or Permission.
func Describe(text string) DescribeOption { return describeOption(text) }

type selectorKeysOption []string

func (k selectorKeysOption) applyType(t *TypeDecl) { t.selectorKeys = append(t.selectorKeys, k...) }
func (k selectorKeysOption) applyRelation(r *relationDecl) {
	r.selectorKeys = append(r.selectorKeys, k...)
}

// TypeAndRelationOption configures either a type or one of its relations.
type TypeAndRelationOption interface {
	TypeOption
	RelationOption
}

// SelectorKeys lists the selector keys, beyond resource_kind and resource_id,
// that stored grants may set. On a type it applies to every relation; on a
// relation it adds keys for that relation only.
func SelectorKeys(keys ...string) TypeAndRelationOption {
	return selectorKeysOption(slices.Clone(keys))
}

type writtenByOption string

func (w writtenByOption) applyType(t *TypeDecl)         { t.writtenBy = string(w) }
func (w writtenByOption) applyRelation(r *relationDecl) { r.writtenBy = string(w) }

// WrittenBy names the grant surface that owns writes for the type's relations,
// or for one relation when set on it.
func WrittenBy(surface string) TypeAndRelationOption { return writtenByOption(surface) }
