package registry

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// The functions in this file derive, from Definitions, every table the
// evaluator and the catalogue read; authzgen writes their results into
// package authz. They hold no policy of their own: a question about what a
// scope means is answered in definitions.go.

// ModelScope is one stored scope and where it comes from.
type ModelScope struct {
	Scope      authz.Scope
	Type       ResourceType
	Permission Permission
	// Block is true for a generated <prefix>:blocked_<action> scope; then
	// Permission is the permission it blocks.
	Block bool
}

// Scopes lists every scope in the model except root: each permission in
// declaration order, followed by its block if it has one.
func (r Registry) Scopes() []ModelScope {
	var out []ModelScope
	for _, t := range r.Types {
		for _, p := range t.Permissions {
			out = append(out, ModelScope{Scope: t.ScopeFor(p), Type: t, Permission: p, Block: false})
			if p.HasBlock() {
				out = append(out, ModelScope{Scope: t.BlockScopeFor(p), Type: t, Permission: p, Block: true})
			}
		}
	}
	return out
}

// HasBlock reports whether p declares a generated block.
func (p Permission) HasBlock() bool {
	return slices.ContainsFunc(p.Exclusions, func(x Exclusion) bool { return x.Kind == ExclusionBlock })
}

// Block returns p's block exclusion, if it declares one.
func (p Permission) Block() (Exclusion, bool) {
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

// DeriveScopeExpansions maps each checked scope to the other scopes whose
// grants also satisfy it. An allow is satisfied by every permission that
// implies it. A block on an implied action also covers the implying one: a
// project:blocked_read grant blocks project:write, so the check for
// project:blocked_write is satisfied by it.
func (r Registry) DeriveScopeExpansions() map[authz.Scope][]authz.Scope {
	out := make(map[authz.Scope][]authz.Scope)
	out[r.Root.Scope] = nil
	for _, s := range r.Scopes() {
		out[s.Scope] = nil
	}
	for _, t := range r.Types {
		for _, higher := range t.Permissions {
			for _, implied := range impliedClosure(t, higher.Action) {
				lower, _ := t.Permission(implied)
				lowerScope := t.ScopeFor(lower)
				out[lowerScope] = append(out[lowerScope], t.ScopeFor(higher))

				if !higher.HasBlock() || !lower.HasBlock() {
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

// DeriveScopeExclusions maps each permission's scope to the scope that stores
// exclusions for it: its generated block, or the exemption it names. Every
// other scope maps to "".
func (r Registry) DeriveScopeExclusions() map[authz.Scope]authz.Scope {
	out := make(map[authz.Scope]authz.Scope)
	out[r.Root.Scope] = ""
	for _, s := range r.Scopes() {
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

// DeriveScopeVisibility marks every declared permission user-visible, and
// generated blocks and root internal.
func (r Registry) DeriveScopeVisibility() map[authz.Scope]string {
	out := make(map[authz.Scope]string)
	out[r.Root.Scope] = authz.ScopeVisibilityInternal
	for _, s := range r.Scopes() {
		if s.Block {
			out[s.Scope] = authz.ScopeVisibilityInternal
			continue
		}
		out[s.Scope] = authz.ScopeVisibilityUserVisible
	}
	return out
}

// DeriveGrantSource maps every scope to where its grants are set.
// A block is written where its permission is.
func (r Registry) DeriveGrantSource() map[authz.Scope]authz.GrantSource {
	out := make(map[authz.Scope]authz.GrantSource)
	out[r.Root.Scope] = authz.GrantSourceRoles
	for _, s := range r.Scopes() {
		out[s.Scope] = s.Permission.Source
	}
	return out
}

// DeriveResourceKinds maps every scope to the resource kind its checks and
// grant selectors name. Root's is the wildcard.
func (r Registry) DeriveResourceKinds() map[authz.Scope]string {
	out := make(map[authz.Scope]string)
	out[r.Root.Scope] = authz.ResourceKindWildcard
	for _, s := range r.Scopes() {
		out[s.Scope] = string(s.Type.Kind)
	}
	return out
}

// DeriveAllowedSelectorKeys maps each resource kind that declares dimensions
// to the keys its grant selectors may carry beyond resource_kind and
// resource_id.
func (r Registry) DeriveAllowedSelectorKeys() map[string]map[string]bool {
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

// DeriveScopeSelectorKeys maps each scope that declares its own dimensions
// to those keys. Blocks share their permission's.
func (r Registry) DeriveScopeSelectorKeys() map[authz.Scope]map[string]bool {
	out := make(map[authz.Scope]map[string]bool)
	for _, s := range r.Scopes() {
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

// DeriveUnknownableDimensions maps each scope to the dimensions its checks
// may carry as SelectorValueUnknown: the optional dimensions of a loosely
// matched type or permission whose exclusions, if any, are blocks. A required
// dimension must be known, a strictly matched type has no optional ones, and
// an unknown value must never reach an exemption, which would grant it.
// project_id is never unknown: a request always knows its tenant.
func (r Registry) DeriveUnknownableDimensions() map[authz.Scope]map[string]bool {
	out := make(map[authz.Scope]map[string]bool)
	for _, t := range r.Types {
		if t.Match == authz.MatchStrict {
			continue
		}
		for _, p := range t.Permissions {
			if slices.ContainsFunc(p.Exclusions, func(x Exclusion) bool { return x.Kind == ExclusionExemption }) || r.isExemptionTarget(t, p) {
				continue
			}
			keys := make(map[string]bool)
			for _, d := range slices.Concat(t.Dimensions, p.Dimensions) {
				if !d.Required && d.Key != authz.SelectorKeyProjectID {
					keys[d.Key] = true
				}
			}
			if len(keys) > 0 {
				out[t.ScopeFor(p)] = keys
				if p.HasBlock() {
					out[t.BlockScopeFor(p)] = keys
				}
			}
		}
	}
	return out
}

// isExemptionTarget reports whether another permission on t names p as its
// exemption.
func (r Registry) isExemptionTarget(t ResourceType, p Permission) bool {
	for _, other := range t.Permissions {
		for _, x := range other.Exclusions {
			if x.Kind == ExclusionExemption && x.Action == p.Action {
				return true
			}
		}
	}
	return false
}

// DeriveRequiredCheckDimensions maps each resource kind to the dimensions
// every one of its checks must carry.
func (r Registry) DeriveRequiredCheckDimensions() map[string][]string {
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

// DeriveDimensionValues returns the closed value set of the dimension key on
// kind, as a lookup.
func (r Registry) DeriveDimensionValues(kind Kind, key string) map[string]bool {
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

// DeriveCatalog lists every grantable scope with its presentation, ordered by
// category and then by declaration, each block right after its permission.
func (r Registry) DeriveCatalog() []authz.ScopeEntry {
	entries := make([]authz.ScopeEntry, 0)
	for _, s := range r.Scopes() {
		entry := authz.ScopeEntry{
			Scope:        s.Scope,
			Label:        s.Permission.Label,
			Category:     s.Type.CategoryFor(s.Permission),
			ResourceType: string(s.Type.Kind),
			Description:  s.Permission.Description,
			Sensitivity:  s.Permission.Sensitivity,
		}
		if s.Block {
			block, _ := s.Permission.Block()
			entry.Label = "Exception: " + lowerFirst(s.Permission.Label)
			entry.Description = "Store exceptions for " + block.Subject + "."
			entry.Sensitivity = authz.SensitivityNone
		}
		entries = append(entries, entry)
	}
	slices.SortStableFunc(entries, func(a, b authz.ScopeEntry) int {
		return cmp.Compare(slices.Index(authz.ScopeCategories, a.Category), slices.Index(authz.ScopeCategories, b.Category))
	})
	return entries
}

// DeriveAgentEligibility maps every scope, root and retired ones included, to
// the registry version it became agent-safe in (0 for never), and whether it
// is retired.
func (r Registry) DeriveAgentEligibility() map[authz.Scope]authz.DerivedAgentEligibility {
	out := make(map[authz.Scope]authz.DerivedAgentEligibility)
	out[r.Root.Scope] = authz.DerivedAgentEligibility{SafeSince: r.Root.Agent.safeSince, Retired: false}
	for _, s := range r.Scopes() {
		agent := s.Permission.Agent
		if s.Block {
			block, _ := s.Permission.Block()
			agent = block.Agent
		}
		out[s.Scope] = authz.DerivedAgentEligibility{SafeSince: agent.safeSince, Retired: false}
	}
	for _, retired := range r.Retired {
		out[retired] = authz.DerivedAgentEligibility{SafeSince: 0, Retired: true}
	}
	return out
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
