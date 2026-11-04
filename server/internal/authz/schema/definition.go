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
//	    schema.GrantSurface("access"),
//	    schema.Grant("viewer", schema.GrantSpec{
//	        StoredAs:    "mcp:read",
//	        Description: "Inspect server configuration",
//	        AnchoredAt:  []string{"organization", "project", schema.Self},
//	        Conditions:  []string{"mcp_tools"},
//	    }),
//	    schema.Block("read_blocked", schema.BlockSpec{
//	        StoredAs:   "mcp:blocked_read",
//	        Conditions: []string{"mcp_tools"},
//	    }),
//	    schema.Permission("read", schema.Rel("viewer"),
//	        schema.AnswersScope("mcp:read"), schema.ExcludedBy("read_blocked")),
//	)
//
// Grant and Block bind a local relation name to a scope that already exists
// in storage, with its catalogue metadata. Declaring one grants nothing:
// stored grants and role defaults decide who holds it.
//
// The package only declares and compiles. It evaluates nothing, performs no
// I/O and imports no service or authz code, so authz can depend on it.
// Parent and AnchoredAt describe containment and where grants may be stored;
// neither verifies ancestry nor makes a parent's permissions apply to a child.
package schema

import (
	"slices"
	"strconv"
)

// Self names the declared type itself in AnchoredAt.
const Self = "$self"

// TypeDecl is the declaration of one resource type, built by NewType and
// consumed by Compile.
type TypeDecl struct {
	name         string
	parent       string
	storedKind   string
	selectorKeys []string
	grantSurface string
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
	kind         relationKind
	name         string
	storedAs     string
	anchors      []string
	conditions   []string
	selectorKeys []string
	description  string
	internal     bool
	sensitive    bool
	strict       bool
	grantSurface string
	roles        []string
	delegatable  int
	impliedBy    []string
}

type permissionDecl struct {
	name        string
	expr        Expr
	direct      []string
	excludedBy  []string
	answers     string
	description string
}

type alternativesDecl struct {
	name      string
	options   []PermissionRef
	blocks    BlockRule
	instances bool
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

// PermissionOption configures a Permission.
type PermissionOption interface {
	applyPermission(p *permissionDecl)
}

type typeOptionFunc func(t *TypeDecl)

func (f typeOptionFunc) applyType(t *TypeDecl) { f(t) }

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
		grantSurface: "",
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

// GrantSpec describes the stored allow scope a Grant binds and its catalogue
// metadata. Only StoredAs is required.
type GrantSpec struct {
	// StoredAs names the existing stored scope the grant reads.
	StoredAs string

	// Description is the catalogue text. Every grant that is not Internal
	// needs one.
	Description string

	// DefaultFor lists the system roles that hold the grant by default.
	DefaultFor []string

	// DelegatableSince records the agent runtime registry version from which
	// the scope may be placed in a delegated runtime policy. Zero means it is
	// never delegatable.
	DelegatableSince int

	// AnchoredAt lists where the grant may be stored: Self or a type on the
	// Parent chain. A grant anchored at a parent applies to every resource of
	// this type under it through selector matching; it does not verify that
	// relationship.
	AnchoredAt []string

	// Conditions lists registered pure conditions every path through the
	// grant must satisfy, such as MCP tool and disposition limits.
	Conditions []string

	// SelectorKeys adds selector keys, beyond the type's, that stored grants
	// of this scope may set.
	SelectorKeys []string

	// Sensitive marks a grant whose holder can read sensitive content.
	Sensitive bool

	// Internal hides the grant from the permission catalogue.
	Internal bool

	// StrictMatching makes the grant match selectors strictly: every key the
	// grant constrains must appear in the check.
	StrictMatching bool

	// GrantSurface overrides the type's GrantSurface for this grant.
	GrantSurface string
}

// BlockSpec describes the stored blocking scope a Block binds. Only StoredAs
// is required.
type BlockSpec struct {
	// StoredAs names the existing stored blocking scope
	// (<resource>:blocked_<action>) the block reads.
	StoredAs string

	// ImpliedBy lists other blocks of the same type that also impose this
	// one. A permission ExcludedBy this block is excluded by them as well, so
	// a read block that also stops writes is declared on the write block.
	ImpliedBy []string

	// Conditions lists registered pure conditions every path through the
	// block must satisfy.
	Conditions []string

	// DelegatableSince records the agent runtime registry version from which
	// the block may be placed in a delegated runtime policy.
	DelegatableSince int

	// GrantSurface overrides the type's GrantSurface for this block.
	GrantSurface string
}

// Grant binds a local relation to an existing stored allow scope and its
// catalogue metadata. It grants nothing: stored grants and role defaults
// decide who holds the scope.
func Grant(name string, spec GrantSpec) TypeOption {
	spec.DefaultFor = slices.Clone(spec.DefaultFor)
	spec.AnchoredAt = slices.Clone(spec.AnchoredAt)
	spec.Conditions = slices.Clone(spec.Conditions)
	spec.SelectorKeys = slices.Clone(spec.SelectorKeys)
	return typeOptionFunc(func(t *TypeDecl) {
		t.relations = append(t.relations, relationDecl{
			kind:         relationGrant,
			name:         name,
			storedAs:     spec.StoredAs,
			anchors:      spec.AnchoredAt,
			conditions:   spec.Conditions,
			selectorKeys: spec.SelectorKeys,
			description:  spec.Description,
			internal:     spec.Internal,
			sensitive:    spec.Sensitive,
			strict:       spec.StrictMatching,
			grantSurface: spec.GrantSurface,
			roles:        spec.DefaultFor,
			delegatable:  spec.DelegatableSince,
			impliedBy:    nil,
		})
	})
}

// Block binds a local relation to an existing stored blocking scope. It
// blocks nothing on its own: stored block grants decide who is blocked, and
// a block applies only through ExcludedBy. Blocks are internal and match
// selectors strictly.
func Block(name string, spec BlockSpec) TypeOption {
	spec.ImpliedBy = slices.Clone(spec.ImpliedBy)
	spec.Conditions = slices.Clone(spec.Conditions)
	return typeOptionFunc(func(t *TypeDecl) {
		t.relations = append(t.relations, relationDecl{
			kind:         relationBlock,
			name:         name,
			storedAs:     spec.StoredAs,
			anchors:      nil,
			conditions:   spec.Conditions,
			selectorKeys: nil,
			description:  "",
			internal:     true,
			sensitive:    false,
			strict:       true,
			grantSurface: spec.GrantSurface,
			roles:        nil,
			delegatable:  spec.DelegatableSince,
			impliedBy:    spec.ImpliedBy,
		})
	})
}

// Permission declares an operation callers check, composed from relations,
// facts and other permissions.
func Permission(name string, expr Expr, opts ...PermissionOption) TypeOption {
	opts = slices.Clone(opts)
	return typeOptionFunc(func(t *TypeDecl) {
		p := permissionDecl{name: name, expr: expr, direct: nil, excludedBy: nil, answers: "", description: ""}
		for _, opt := range opts {
			opt.applyPermission(&p)
		}
		t.permissions = append(t.permissions, p)
	})
}

// BlockRule says how the blocks of Alternatives' options combine. Every
// Alternatives must choose one; the zero value is a declaration error.
type BlockRule int

const (
	blockRuleUnset BlockRule = iota

	// AnyBlockVetoes rejects the request when any option's ExcludedBy block
	// matches in any policy, even if another option passes.
	AnyBlockVetoes

	// BlocksApplyPerOption lets a block stop only its own option, so another
	// unblocked option can still pass.
	BlocksApplyPerOption
)

// String returns the rule's name.
func (r BlockRule) String() string {
	switch r {
	case blockRuleUnset:
		return "unset"
	case AnyBlockVetoes:
		return "AnyBlockVetoes"
	case BlocksApplyPerOption:
		return "BlocksApplyPerOption"
	default:
		return "BlockRule(" + strconv.Itoa(int(r)) + ")"
	}
}

// AlternativesSpec describes named Alternatives.
type AlternativesSpec struct {
	// Options lists the complete permission checks, in order.
	Options []PermissionRef

	// Blocks says how the options' blocks combine. It must be set.
	Blocks BlockRule

	// AcrossInstances lets Options name the same permission more than once,
	// because each position is asked of a different resource instance: for
	// example a grant stored against the whole collection, or one against a
	// single member. The typed constructor supplies the instance for each
	// position. Without it a repeated option is a declaration error.
	AcrossInstances bool
}

// Alternatives declares a named list of complete permission checks. A caller
// passes when one option passes in every policy; it is not a Union, so
// different policies cannot each satisfy a different option.
func Alternatives(name string, spec AlternativesSpec) TypeOption {
	options := slices.Clone(spec.Options)
	return typeOptionFunc(func(t *TypeDecl) {
		t.alternatives = append(t.alternatives, alternativesDecl{
			name:      name,
			options:   options,
			blocks:    spec.Blocks,
			instances: spec.AcrossInstances,
		})
	})
}

// ExcludedBy applies block precedence to the permission: a matching block
// removes access unless a qualifying direct user grant restores the matching
// instance, and the principal's own blocks always win. The named block is
// the permission's direct block; the blocks that imply it, through
// BlockSpec.ImpliedBy, exclude the permission too.
func ExcludedBy(block string) PermissionOption {
	return permissionOptionFunc(func(p *permissionDecl) { p.direct = append(p.direct, block) })
}

// AnswersScope names the stored scope whose existing authz.Check this
// permission answers. The permission must be grant-only, so a check routed to
// it keeps today's grant semantics.
func AnswersScope(scope string) PermissionOption {
	return permissionOptionFunc(func(p *permissionDecl) { p.answers = scope })
}

// Describe sets the catalogue description of a Permission.
func Describe(text string) PermissionOption {
	return permissionOptionFunc(func(p *permissionDecl) { p.description = text })
}

// SelectorKeys lists the selector keys, beyond resource_kind and resource_id,
// that stored grants of every relation on the type may set.
func SelectorKeys(keys ...string) TypeOption {
	keys = slices.Clone(keys)
	return typeOptionFunc(func(t *TypeDecl) { t.selectorKeys = append(t.selectorKeys, keys...) })
}

// GrantSurface names the grant surface that owns writes for the type's
// relations. A Grant or Block can override it in its spec.
func GrantSurface(surface string) TypeOption {
	return typeOptionFunc(func(t *TypeDecl) { t.grantSurface = surface })
}
