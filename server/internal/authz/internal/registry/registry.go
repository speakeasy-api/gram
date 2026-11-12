// Package registry declares the authorization model: every resource type, its
// permissions, blocks and dimensions, and the built-in roles. It is build-time
// input only. authzgen reads Definitions and writes the scope constants, the
// typed refs and every derived table into package authz, which never imports
// this package. Being internal to authz, it cannot be imported from anywhere
// else.
package registry

import "github.com/speakeasy-api/gram/server/internal/authz"

// This file holds the types of the authorization model. The model itself, one
// Go literal describing every resource type and its permissions, is
// Definitions in definitions.go.
//
// Vocabulary:
//
//   - A Kind is a type of resource a grant can name: org, project, mcp, ...
//   - An Action is the verb: read, write, connect, ...
//   - A Permission is the definition of one action on one resource type: what
//     it implies, what can subtract from it, who may hold it.
//   - A Scope is the stored string a grant carries, kind:action by default
//     ("mcp:write"). A few permissions keep a historical string whose prefix is
//     not their kind (plugin:write and log:read are checked against a project,
//     mcp:auth against an auth client); they set Permission.LegacyScope.
//   - An Exclusion is a grant that subtracts from a permission. A Block
//     (mcp:blocked_write) takes access away and is generated from the
//     permission; an Exemption (risk_policy:bypass) exempts its holder from a
//     policy and names another declared permission.

// Kind names a resource type, for example "mcp".
type Kind string

// Action names what a permission allows on its resource type, for example
// "write".
type Action string

// ExclusionKind distinguishes exclusions that take access away from ones that
// exempt a holder from a policy.
type ExclusionKind int

const (
	// ExclusionBlock takes access away. A direct grant naming a concrete
	// resource outranks a block inherited from a role or user:all.
	ExclusionBlock ExclusionKind = iota + 1
	// ExclusionExemption exempts its holder from a policy. Direct grants never
	// cancel an exemption.
	ExclusionExemption
)

// AgentEligibility says whether agent direct and delegated policies may hold a
// scope, and since which agent runtime scope registry version. The zero value
// is "registered, never agent-safe".
type AgentEligibility struct {
	safeSince int
}

// SafeSince marks a scope agent-runtime-safe from registry version onwards.
// Policies stored at an older version keep reading it as unsafe.
func SafeSince(version int) AgentEligibility {
	if version < 1 {
		panic("authz.SafeSince: version must be at least 1; use AgentUnsafe for a scope that is never agent-safe")
	}
	return AgentEligibility{safeSince: version}
}

// AgentUnsafe marks a scope agent policy may never hold.
func AgentUnsafe() AgentEligibility {
	return AgentEligibility{safeSince: 0}
}

// SafeSinceVersion is the registry version a scope became agent-safe in, or 0
// if it never did.
func (a AgentEligibility) SafeSinceVersion() int { return a.safeSince }

// Exclusion is a grant that subtracts from the permission declaring it.
type Exclusion struct {
	Kind ExclusionKind
	// Action is the action of the exemption's own permission, which must be
	// declared on the same resource type. A block leaves it empty: its stored
	// scope is generated as <prefix>:blocked_<action> from the permission.
	Action Action
	// Agent is the generated block scope's agent eligibility. Exemptions use
	// the eligibility of the permission they name.
	Agent AgentEligibility
	// Subject completes "Store exceptions for ..." in a generated block's
	// catalogue description, for example "MCP write access".
	Subject string
}

// Blocked declares the standard block for a permission: a generated
// <prefix>:blocked_<action> scope, agent-eligible as given, described as
// storing exceptions for subject.
func Blocked(agent AgentEligibility, subject string) []Exclusion {
	return []Exclusion{{Kind: ExclusionBlock, Action: "", Agent: agent, Subject: subject}}
}

// Dimension is a selector key a grant on a resource type may carry beyond
// resource_kind and resource_id.
type Dimension struct {
	Key string
	// Values is the closed set of values the key accepts, or nil for free
	// text. A wildcard is always accepted where the key allows one.
	Values []string
	// Required marks a key every check of the type must carry with a concrete
	// value. Without it, a loosely matched check that omits the key still
	// matches a grant constrained on it.
	Required bool
}

// DimensionSet names a request method that carries dimensions describing one
// use of the resource rather than the resource itself: connect's ToolCall
// takes the tool and its disposition. A value left empty is sent as unknown,
// so a grant limited on that key never allows it and a block on it applies.
type DimensionSet struct {
	Method string
	// Keys are dimensions declared on the resource type or the permission,
	// in the order the method takes them.
	Keys []string
}

// legacyScopes are the only stored strings a permission may keep through
// LegacyScope. They predate the registry and are stored in grants, API key
// and agent policies, and the public scope enum. They go, with the field,
// when R1 renames the scopes.
var legacyScopes = []authz.Scope{"plugin:write", "log:read", "mcp:auth"}

// Permission defines one action on a resource type.
type Permission struct {
	Action Action
	// LegacyScope keeps a stored string that is not kind:action. Only the
	// scopes in legacyScopes may use it; new permissions take the default.
	LegacyScope authz.Scope
	// Implies lists actions on the same resource type a grant of this one
	// also satisfies. The closure is computed; list direct implications only.
	Implies []Action
	// Exclusions subtract from this permission. Blocks covering an implied
	// action also cover this one (they follow Implies in reverse).
	Exclusions []Exclusion
	// Dimensions are selector keys allowed on this permission only, beyond
	// its resource type's.
	Dimensions []Dimension
	// DimensionSets are extra request methods on this permission's ref, each
	// carrying a fixed set of the type's dimensions as arguments, beside the
	// plain method named after the action.
	DimensionSets []DimensionSet
	// Source says where grants of this permission's scopes are set: through
	// roles or through risk policies. Every permission sets it explicitly.
	Source authz.GrantSource
	// Category overrides the resource type's dashboard grouping.
	Category authz.ScopeCategory
	Label    string
	// Description says what a holder can do and, where it matters, what the
	// permission does not include.
	Description string
	Sensitivity authz.Sensitivity
	Agent       AgentEligibility
}

// ResourceType declares a kind of resource and every permission on it.
type ResourceType struct {
	Kind Kind
	// Parent is the kind that contains this one: mcp sits in a project, a
	// project in an org. It is documentation today; typed request refs take
	// the parent's id as a required argument.
	Parent      Kind
	Category    authz.ScopeCategory
	Match       authz.MatchMode
	Dimensions  []Dimension
	Permissions []Permission
}

// Registry is the whole authorization model.
type Registry struct {
	// Root is the engine's own bypass scope. It is internal and never
	// presented.
	Root RootPermission
	// Types lists every resource type in declaration order.
	Types []ResourceType
	// SystemRoles lists the scopes each built-in role is seeded with, in
	// order. Seeding only applies to new orgs: role grants an org has edited
	// are never rewritten.
	SystemRoles map[string][]authz.Scope
	// Retired lists scope names that no longer exist. They stay reserved so
	// stored policies holding them still parse, and must never be reused.
	Retired []authz.Scope
}

// RootPermission describes the root scope.
type RootPermission struct {
	Scope authz.Scope
	Agent AgentEligibility
}
