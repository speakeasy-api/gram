package zedspike

import core "github.com/authzed/spicedb/pkg/proto/core/v1"

// Registry is the compiled form of rbac.zed joined with the Go tables: what
// the evaluator, the grant loader, the old-table projection and the rendered
// documentation read. It holds no grant data.
type Registry struct {
	Types   map[string]*TypeDef
	Caveats map[string]*CaveatDef
}

// TypeDef is one definition in the schema, such as mcp_server.
type TypeDef struct {
	Name        string
	Relations   map[string]*RelationDef
	Permissions map[string]*PermissionDef
}

// RelationKind says what rows a relation holds, read from its name and its
// subject types.
type RelationKind int

const (
	// RelationGrant holds allow rows of one stored scope.
	RelationGrant RelationKind = iota + 1
	// RelationDirect holds the same allow rows when they were written for one
	// user by name and name this resource.
	RelationDirect
	// RelationBlock holds block rows written for a role or for everyone.
	RelationBlock
	// RelationOwnBlock holds block rows written for the user being checked.
	RelationOwnBlock
	// RelationEdge points at another resource; never stored.
	RelationEdge
	// RelationFact holds a column of the checked row, such as chats.user_id;
	// never stored as a grant.
	RelationFact
	// RelationMembership is role#member, read from role assignments.
	RelationMembership
)

// RelationDef is one relation of a type.
type RelationDef struct {
	Name string
	Kind RelationKind
	// Scope is the stored scope the relation's rows carry. Empty for edges,
	// facts and memberships.
	Scope Scope
	// Subjects lists the subject types the relation accepts, such as "user",
	// "user:*", "role#member", "agent" and "api_key", each with its caveat.
	Subjects []SubjectType
}

// SubjectType is one allowed subject of a relation.
type SubjectType struct {
	Type     string // user, role, agent, api_key
	Relation string // "member" for role#member
	Wildcard bool   // user:*
	Caveat   string
}

// PermissionDef is one permission with its expression.
type PermissionDef struct {
	Name string
	// Internal permissions (has_*, blocked_*) are building blocks that typed
	// methods cannot name.
	Internal bool
	Expr     Expr
}

// Expr is a permission expression. Exactly one field is set.
type Expr struct {
	Union     []Expr
	Intersect []Expr
	// Exclude is base minus subtracted.
	Exclude *Exclusion
	// Ref names a relation or permission on the same type.
	Ref string
	// Arrow follows an edge relation to a relation or permission on the
	// type it points at.
	Arrow *Arrow
}

// Exclusion is base minus subtracted.
type Exclusion struct {
	Base       Expr
	Subtracted Expr
}

// Arrow is edge->target.
type Arrow struct {
	Edge   string
	Target string
}

// CaveatDef is one caveat with its parameter names. The expression itself is
// SpiceDB's CEL and is evaluated with SpiceDB's caveat package.
type CaveatDef struct {
	Name string
	// Definition is SpiceDB's compiled caveat, expression included, ready
	// for its caveats package to evaluate.
	Definition *core.CaveatDefinition
	// GrantedParams are filled from the grant row's selector.
	GrantedParams []string
	// CheckParams are filled from the check.
	CheckParams []string
}
