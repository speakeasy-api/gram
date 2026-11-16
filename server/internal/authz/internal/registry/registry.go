// Package registry declares the authorization model: every resource type, its
// permissions, blocks and dimensions, and the built-in roles. It is build-time
// input only. authzgen reads Definitions and writes the scope constants, the
// typed refs and every derived table into package authz, which never imports
// this package, and this package imports nothing from authz: the generated Go
// is the only thing that passes between them. The names both sides use are
// declared once, in the vocabulary files (see VocabularyFiles).
package registry

import (
	"embed"
	"strings"
)

// VocabularyFile is one registry source file that authzgen copies into
// package authz.
type VocabularyFile struct {
	// Name is the file's name in this package, for example "selectors.go".
	Name string
	// Source is the file as written.
	Source string
}

//go:embed scopes.go selectors.go sensitivity.go grant_source.go roles.go match_mode.go
var vocabularyFS embed.FS

// VocabularyFiles returns the files that declare the names shared with
// package authz, in a fixed order. Each holds one subject, so a new selector
// key goes in selectors.go and a new action in scopes.go.
func VocabularyFiles() []VocabularyFile {
	names := []string{"scopes.go", "selectors.go", "sensitivity.go", "grant_source.go", "roles.go", "match_mode.go"}
	files := make([]VocabularyFile, 0, len(names))
	for _, name := range names {
		source, err := vocabularyFS.ReadFile(name)
		if err != nil {
			panic("registry: embedded vocabulary file " + name + ": " + err.Error())
		}
		files = append(files, VocabularyFile{Name: name, Source: string(source)})
	}
	return files
}

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

// Scope is the stored string a grant carries, as the model writes it. Package
// authz has its own hand-written Scope type for the evaluator; the generator
// writes these values into authz as Scope constants and table entries.
type Scope string

// split cuts a scope into its prefix and action at the first colon.
func (s Scope) split() (prefix, action string) {
	prefix, action, _ = strings.Cut(string(s), ":")
	return prefix, action
}

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
		panic("registry.SafeSince: version must be at least 1; use AgentUnsafe for a scope that is never agent-safe")
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

// legacyScopes are the only stored strings a permission may keep through
// LegacyScope. They predate the registry and are stored in grants, API key
// and agent policies, and the public scope enum. They go, with the field,
// when R1 renames the scopes. The memory and channels scopes were added
// alongside the registry and are checked against a project in the same way.
var legacyScopes = []Scope{
	"plugin:write", "log:read", "mcp:auth",
	"memory:read", "memory:write",
	"channels:connect", "channels:admin", "channels:read_all",
}

// Permission defines one action on a resource type.
type Permission struct {
	Action Action
	// LegacyScope keeps a stored string that is not kind:action. Only the
	// scopes in legacyScopes may use it; new permissions take the default.
	LegacyScope Scope
	// Implies lists actions on the same resource type a grant of this one
	// also satisfies. The closure is computed; list direct implications only.
	Implies []Action
	// Exclusions subtract from this permission. Blocks covering an implied
	// action also cover this one (they follow Implies in reverse).
	Exclusions []Exclusion
	// Dimensions are selector keys allowed on this permission only, beyond
	// its resource type's.
	Dimensions []Dimension
	// Source says where grants of this permission's scopes are set: through
	// roles or through risk policies. Every permission sets it explicitly.
	Source GrantSource
	// Category overrides the resource type's dashboard grouping.
	Category ScopeCategory
	Label    string
	// Description says what a holder can do and, where it matters, what the
	// permission does not include.
	Description string
	Sensitivity Sensitivity
	Agent       AgentEligibility
}

// ResourceType declares a kind of resource and every permission on it.
type ResourceType struct {
	Kind Kind
	// Parent is the kind that contains this one: mcp sits in a project, a
	// project in an org. It is documentation today; typed request refs take
	// the parent's id as a required argument.
	Parent      Kind
	Category    ScopeCategory
	Match       MatchMode
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
	SystemRoles map[string][]Scope
	// Retired lists scope names that no longer exist. They stay reserved so
	// stored policies holding them still parse, and must never be reused.
	Retired []Scope
}

// RootPermission describes the root scope.
type RootPermission struct {
	Scope Scope
	Agent AgentEligibility
}
