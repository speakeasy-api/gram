package authz

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The functions in this file derive, from Model, every table the evaluator
// and the catalogue read. They hold no policy of their own: a question about
// what a scope means is answered in registry_resources.go.

// independentBlockKinds lists the kinds whose blocks do not yet follow
// Implies in reverse. Today mcp:blocked_read does not block mcp:write; the
// plan treats that as a bug (R7) and fixes it as its own behaviour change, by
// emptying this list. Until then the derived tables keep today's behaviour.
var independentBlockKinds = []Kind{"mcp"}

// modelScope is one stored scope and where it comes from.
type modelScope struct {
	Scope      Scope
	Type       ResourceType
	Permission Permission
	// Block is true for a generated <prefix>:blocked_<action> scope; then
	// Permission is the permission it blocks.
	Block bool
}

// modelScopes lists every scope in the model except root: each permission in
// declaration order, followed by its block if it has one.
func (r Registry) modelScopes() []modelScope {
	var out []modelScope
	for _, t := range r.Types {
		for _, p := range t.Permissions {
			out = append(out, modelScope{Scope: t.ScopeFor(p), Type: t, Permission: p, Block: false})
			if hasBlock(p) {
				out = append(out, modelScope{Scope: t.BlockScopeFor(p), Type: t, Permission: p, Block: true})
			}
		}
	}
	return out
}

func hasBlock(p Permission) bool {
	return slices.ContainsFunc(p.Exclusions, func(x Exclusion) bool { return x.Kind == ExclusionBlock })
}

func blockOf(p Permission) (Exclusion, bool) {
	i := slices.IndexFunc(p.Exclusions, func(x Exclusion) bool { return x.Kind == ExclusionBlock })
	if i < 0 {
		var none Exclusion
		return none, false
	}
	return p.Exclusions[i], true
}

// impliedClosure returns every action on t that a grant of action satisfies,
// action itself excluded.
func impliedClosure(t ResourceType, action Action) []Action {
	seen := map[Action]bool{action: true}
	queue := []Action{action}
	var out []Action
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		p, ok := t.Permission(current)
		if !ok {
			continue
		}
		for _, next := range p.Implies {
			if seen[next] {
				continue
			}
			seen[next] = true
			out = append(out, next)
			queue = append(queue, next)
		}
	}
	return out
}

// deriveScopeExpansions maps each checked scope to the other scopes whose
// grants also satisfy it. An allow is satisfied by every permission that
// implies it. A block on an implied action also covers the implying one: a
// project:blocked_read grant blocks project:write, so the check for
// project:blocked_write is satisfied by it.
func (r Registry) deriveScopeExpansions() map[Scope][]Scope {
	out := make(map[Scope][]Scope)
	out[r.Root.Scope] = nil
	for _, s := range r.modelScopes() {
		out[s.Scope] = nil
	}
	for _, t := range r.Types {
		for _, higher := range t.Permissions {
			for _, implied := range impliedClosure(t, higher.Action) {
				lower, _ := t.Permission(implied)
				lowerScope := t.ScopeFor(lower)
				out[lowerScope] = append(out[lowerScope], t.ScopeFor(higher))

				if slices.Contains(independentBlockKinds, t.Kind) || !hasBlock(higher) || !hasBlock(lower) {
					continue
				}
				higherBlock := t.BlockScopeFor(higher)
				out[higherBlock] = append(out[higherBlock], t.BlockScopeFor(lower))
			}
		}
	}
	for scope, satisfying := range out {
		if len(satisfying) == 0 {
			out[scope] = nil
			continue
		}
		slices.Sort(satisfying)
		out[scope] = slices.Compact(satisfying)
	}
	return out
}

// deriveScopeExclusions maps each permission's scope to the scope that stores
// exclusions for it: its generated block, or the exemption it names. Every
// other scope maps to "".
func (r Registry) deriveScopeExclusions() map[Scope]Scope {
	out := make(map[Scope]Scope)
	out[r.Root.Scope] = ""
	for _, s := range r.modelScopes() {
		out[s.Scope] = ""
	}
	for _, t := range r.Types {
		for _, p := range t.Permissions {
			for _, x := range p.Exclusions {
				switch x.Kind {
				case ExclusionBlock:
					out[t.ScopeFor(p)] = t.BlockScopeFor(p)
				case ExclusionExemption:
					target, _ := t.Permission(x.Action)
					out[t.ScopeFor(p)] = t.ScopeFor(target)
				}
			}
		}
	}
	return out
}

// deriveScopeVisibility marks every declared permission user-visible, and
// generated blocks and root internal.
func (r Registry) deriveScopeVisibility() map[Scope]scopeVisibility {
	out := make(map[Scope]scopeVisibility)
	out[r.Root.Scope] = scopeVisibilityInternal
	for _, s := range r.modelScopes() {
		if s.Block {
			out[s.Scope] = scopeVisibilityInternal
			continue
		}
		out[s.Scope] = scopeVisibilityUserVisible
	}
	return out
}

// deriveGrantSource maps every scope to where its grants are set.
// A block is written where its permission is.
func (r Registry) deriveGrantSource() map[Scope]GrantSource {
	out := make(map[Scope]GrantSource)
	out[r.Root.Scope] = GrantSourceRoles
	for _, s := range r.modelScopes() {
		out[s.Scope] = s.Permission.Source
	}
	return out
}

// deriveResourceKinds maps every scope to the resource kind its checks and
// grant selectors name. Root's is the wildcard.
func (r Registry) deriveResourceKinds() map[Scope]string {
	out := make(map[Scope]string)
	out[r.Root.Scope] = ResourceKindWildcard
	for _, s := range r.modelScopes() {
		out[s.Scope] = string(s.Type.Kind)
	}
	return out
}

// deriveAllowedSelectorKeys maps each resource kind that declares dimensions
// to the keys its grant selectors may carry beyond resource_kind and
// resource_id.
func (r Registry) deriveAllowedSelectorKeys() map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	for _, t := range r.Types {
		if len(t.Dimensions) == 0 {
			continue
		}
		keys := make(map[string]bool, len(t.Dimensions))
		for _, d := range t.Dimensions {
			keys[d.Key] = true
		}
		out[string(t.Kind)] = keys
	}
	return out
}

// deriveScopeSelectorKeys maps each scope that declares its own dimensions
// to those keys. Blocks share their permission's.
func (r Registry) deriveScopeSelectorKeys() map[Scope]map[string]bool {
	out := make(map[Scope]map[string]bool)
	for _, s := range r.modelScopes() {
		if len(s.Permission.Dimensions) == 0 {
			continue
		}
		keys := make(map[string]bool, len(s.Permission.Dimensions))
		for _, d := range s.Permission.Dimensions {
			keys[d.Key] = true
		}
		out[s.Scope] = keys
	}
	return out
}

// deriveRequiredCheckDimensions maps each resource kind to the dimensions
// every one of its checks must carry.
func (r Registry) deriveRequiredCheckDimensions() map[string][]string {
	out := make(map[string][]string)
	for _, t := range r.Types {
		for _, d := range t.Dimensions {
			if d.Required {
				out[string(t.Kind)] = append(out[string(t.Kind)], d.Key)
			}
		}
	}
	return out
}

// deriveDimensionValues returns the closed value set of the dimension key on
// kind, as a lookup.
func (r Registry) deriveDimensionValues(kind Kind, key string) map[string]bool {
	out := make(map[string]bool)
	for _, t := range r.Types {
		if t.Kind != kind {
			continue
		}
		for _, d := range t.Dimensions {
			if d.Key != key {
				continue
			}
			for _, v := range d.Values {
				out[v] = true
			}
		}
	}
	return out
}

// deriveSystemRoleScopes returns a copy of a system role's seeded scopes.
func (r Registry) deriveSystemRoleScopes(role string) []Scope {
	return slices.Clone(r.SystemRoles[role])
}

// deriveCatalog lists every grantable scope with its presentation, ordered by
// category and then by declaration, each block right after its permission.
func (r Registry) deriveCatalog() []ScopeEntry {
	entries := make([]ScopeEntry, 0)
	for _, s := range r.modelScopes() {
		entry := ScopeEntry{
			Scope:        s.Scope,
			Label:        s.Permission.Label,
			Category:     s.Type.CategoryFor(s.Permission),
			ResourceType: string(s.Type.Kind),
			Description:  s.Permission.Description,
			Sensitivity:  s.Permission.Sensitivity,
		}
		if s.Block {
			block, _ := blockOf(s.Permission)
			entry.Label = "Exception: " + lowerFirst(s.Permission.Label)
			entry.Description = "Store exceptions for " + block.Subject + "."
			entry.Sensitivity = SensitivityNone
		}
		entries = append(entries, entry)
	}
	slices.SortStableFunc(entries, func(a, b ScopeEntry) int {
		return cmp.Compare(slices.Index(ScopeCategories, a.Category), slices.Index(ScopeCategories, b.Category))
	})
	return entries
}

// deriveAgentEligibility maps every scope, root and retired ones included, to
// the registry version it became agent-safe in (0 for never), and whether it
// is retired.
func (r Registry) deriveAgentEligibility() map[Scope]DerivedAgentEligibility {
	out := make(map[Scope]DerivedAgentEligibility)
	out[r.Root.Scope] = DerivedAgentEligibility{SafeSince: r.Root.Agent.safeSince, Retired: false}
	for _, s := range r.modelScopes() {
		agent := s.Permission.Agent
		if s.Block {
			block, _ := blockOf(s.Permission)
			agent = block.Agent
		}
		out[s.Scope] = DerivedAgentEligibility{SafeSince: agent.safeSince, Retired: false}
	}
	for _, retired := range r.Retired {
		out[retired] = DerivedAgentEligibility{SafeSince: 0, Retired: true}
	}
	return out
}

// DerivedAgentEligibility is one scope's agent eligibility as the model
// declares it.
type DerivedAgentEligibility struct {
	// SafeSince is the agent runtime scope registry version the scope became
	// agent-safe in, or 0 if it never did.
	SafeSince int
	// Retired marks a tombstone: the scope no longer exists.
	Retired bool
}

// AgentEligibilityByScope returns the agent eligibility of every scope the
// model knows, retired tombstones included. The agent runtime scope registry
// reads it.
func AgentEligibilityByScope() map[Scope]DerivedAgentEligibility {
	return Model.deriveAgentEligibility()
}

func lowerFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	// Leave acronyms such as "MCP" alone.
	if size < len(s) {
		next, _ := utf8.DecodeRuneInString(s[size:])
		if unicode.IsUpper(next) {
			return s
		}
	}
	return strings.ToLower(string(r)) + s[size:]
}
