package registry

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// ScopeFor returns the stored scope of permission p on resource type t.
func (t ResourceType) ScopeFor(p Permission) authz.Scope {
	if p.LegacyScope != "" {
		return p.LegacyScope
	}
	return authz.Scope(string(t.Kind) + ":" + string(p.Action))
}

// BlockScopeFor returns the generated block scope paired with permission p on
// resource type t: the stored scope's prefix, then blocked_ and its action.
func (t ResourceType) BlockScopeFor(p Permission) authz.Scope {
	parts := t.ScopeFor(p).Parts()
	return authz.Scope(parts.Resource + ":blocked_" + parts.Action)
}

// Permission returns the permission for action, if declared.
func (t ResourceType) Permission(action Action) (Permission, bool) {
	for _, p := range t.Permissions {
		if p.Action == action {
			return p, true
		}
	}
	var none Permission
	return none, false
}

// CategoryFor is the dashboard grouping of permission p on t.
func (t ResourceType) CategoryFor(p Permission) authz.ScopeCategory {
	if p.Category != "" {
		return p.Category
	}
	return t.Category
}

// Validate reports every structural error in the model. It is what makes the
// registry safe to edit: each rule below is one a hand-kept table could break
// silently.
func (r Registry) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if r.Root.Scope == "" {
		fail("root scope is empty")
	}
	scopes := make(map[authz.Scope]string)
	scopes[r.Root.Scope] = "root"
	claim := func(scope authz.Scope, owner string) {
		if previous, ok := scopes[scope]; ok {
			fail("scope %q is declared by both %s and %s", scope, previous, owner)
			return
		}
		scopes[scope] = owner
	}

	kinds := map[Kind]bool{}
	for _, t := range r.Types {
		if t.Kind == "" {
			fail("a resource type has no kind")
			continue
		}
		if kinds[t.Kind] {
			fail("resource type %q is declared twice", t.Kind)
		}
		kinds[t.Kind] = true
	}

	for _, t := range r.Types {
		if t.Parent != "" && !kinds[t.Parent] {
			fail("%s: parent %q is not a declared resource type", t.Kind, t.Parent)
		}
		if t.Category == "" {
			fail("%s: no category", t.Kind)
		} else if !slices.Contains(authz.ScopeCategories, t.Category) {
			fail("%s: unknown category %q", t.Kind, t.Category)
		}
		dimKeys := map[string]bool{}
		for _, d := range t.Dimensions {
			validateDimension(fail, string(t.Kind), d)
			if dimKeys[d.Key] {
				fail("%s: dimension %q is declared twice", t.Kind, d.Key)
			}
			dimKeys[d.Key] = true
		}

		actions := map[Action]Permission{}
		for _, p := range t.Permissions {
			if p.Action == "" {
				fail("%s: a permission has no action", t.Kind)
				continue
			}
			if _, dup := actions[p.Action]; dup {
				fail("%s: action %q is declared twice", t.Kind, p.Action)
			}
			if p.LegacyScope != "" && !slices.Contains(legacyScopes, p.LegacyScope) {
				fail("%s.%s: legacy scope %q is not one of the historical scopes; new permissions use kind:action", t.Kind, p.Action, p.LegacyScope)
			}
			actions[p.Action] = p
		}

		// Ref methods share one namespace per kind: an action's method and
		// every dimension set's.
		methods := map[string]bool{}
		for _, p := range t.Permissions {
			methods[camelAction(p.Action)] = true
		}
		for _, p := range t.Permissions {
			owner := fmt.Sprintf("%s.%s", t.Kind, p.Action)
			scope := t.ScopeFor(p)
			if parts := scope.Parts(); parts.Resource == "" || parts.Action == "" || strings.Count(string(scope), ":") != 1 {
				fail("%s: scope %q is not resource:action", owner, scope)
			}
			if strings.HasPrefix(scope.Parts().Action, "blocked_") {
				fail("%s: blocked_ actions are generated, not declared", owner)
			}
			claim(scope, owner)
			if p.Label == "" || p.Description == "" {
				fail("%s: label and description are required", owner)
			}
			if p.Source != authz.GrantSourceRoles && p.Source != authz.GrantSourceRiskPolicy {
				fail("%s: Source must be GrantSourceRoles or GrantSourceRiskPolicy, got %q", owner, p.Source)
			}
			if c := t.CategoryFor(p); !slices.Contains(authz.ScopeCategories, c) {
				fail("%s: unknown category %q", owner, c)
			}
			permissionDimKeys := map[string]bool{}
			for _, d := range p.Dimensions {
				validateDimension(fail, owner, d)
				if permissionDimKeys[d.Key] {
					fail("%s: dimension %q is declared twice", owner, d.Key)
				}
				if dimKeys[d.Key] {
					fail("%s: dimension %q is already declared on %s", owner, d.Key, t.Kind)
				}
				permissionDimKeys[d.Key] = true
			}

			for _, set := range p.DimensionSets {
				if set.Method == "" || len(set.Keys) == 0 {
					fail("%s: a dimension set needs a method name and at least one key", owner)
					continue
				}
				if methods[set.Method] {
					fail("%s: dimension set method %q clashes with another method on %s", owner, set.Method, t.Kind)
				}
				methods[set.Method] = true
				seen := map[string]bool{}
				for _, key := range set.Keys {
					if !dimKeys[key] && !permissionDimKeys[key] {
						fail("%s: dimension set %s uses %q, which is not a dimension of %s", owner, set.Method, key, t.Kind)
					}
					if seen[key] {
						fail("%s: dimension set %s lists %q twice", owner, set.Method, key)
					}
					seen[key] = true
				}
			}

			for _, implied := range p.Implies {
				target, ok := actions[implied]
				if !ok {
					fail("%s: implies unknown action %q", owner, implied)
					continue
				}
				// An agent may hold this permission only if it may hold
				// everything the permission satisfies, from the same version.
				if p.Agent.safeSince != 0 && (target.Agent.safeSince == 0 || target.Agent.safeSince > p.Agent.safeSince) {
					fail("%s: agent-safe since v%d but implies %s, which is not", owner, p.Agent.safeSince, implied)
				}
			}

			blocks, exemptions := 0, 0
			for _, x := range p.Exclusions {
				switch x.Kind {
				case ExclusionBlock:
					blocks++
					if x.Action != "" {
						fail("%s: a block's scope is generated; leave Action empty", owner)
					}
					if x.Subject == "" {
						fail("%s: a block needs a Subject for its description", owner)
					}
					claim(t.BlockScopeFor(p), owner+" (block)")
				case ExclusionExemption:
					exemptions++
					target, ok := actions[x.Action]
					switch {
					case !ok:
						fail("%s: exemption names unknown action %q", owner, x.Action)
					case target.Action == p.Action:
						fail("%s: a permission cannot exempt itself", owner)
					case target.Source != p.Source:
						fail("%s: exemption %q has a different grant source", owner, x.Action)
					}
				default:
					fail("%s: exclusion has no kind", owner)
				}
			}
			if blocks > 1 {
				fail("%s: at most one block per permission", owner)
			}
			// A permission has one exclusion scope, so it is either blocked or
			// exempted, never both.
			if blocks+exemptions > 1 {
				fail("%s: at most one exclusion per permission", owner)
			}
		}

		if cycle := impliesCycle(t); cycle != "" {
			fail("%s: Implies has a cycle through %s", t.Kind, cycle)
		}
	}

	for _, retired := range r.Retired {
		if owner, ok := scopes[retired]; ok {
			fail("retired scope %q is reused by %s", retired, owner)
		}
	}

	for role, roleScopes := range r.SystemRoles {
		seen := make(map[authz.Scope]bool)
		for _, scope := range roleScopes {
			owner, ok := scopes[scope]
			switch {
			case !ok:
				fail("system role %q names unknown scope %q", role, scope)
			case strings.HasSuffix(owner, "(block)") || owner == "root":
				fail("system role %q names %q, which is not a grantable permission", role, scope)
			}
			if seen[scope] {
				fail("system role %q names %q twice", role, scope)
			}
			seen[scope] = true
		}
	}

	return errors.Join(errs...)
}

func validateDimension(fail func(string, ...any), owner string, d Dimension) {
	if d.Key == "" || d.Key == authz.SelectorKeyResourceKind || d.Key == authz.SelectorKeyResourceID {
		fail("%s: dimension key %q is reserved or empty", owner, d.Key)
	}
	for _, v := range d.Values {
		if v == "" || v == authz.WildcardResource {
			fail("%s: dimension %q lists %q as a value", owner, d.Key, v)
		}
	}
}

// impliesCycle returns the action a cycle in t's Implies graph passes
// through, or "" when there is none.
func impliesCycle(t ResourceType) string {
	const (
		unvisited = iota
		visiting
		done
	)
	state := map[Action]int{}
	edges := map[Action][]Action{}
	for _, p := range t.Permissions {
		edges[p.Action] = p.Implies
	}
	var visit func(a Action) string
	visit = func(a Action) string {
		switch state[a] {
		case visiting:
			return string(a)
		case done:
			return ""
		}
		state[a] = visiting
		for _, next := range edges[a] {
			if c := visit(next); c != "" {
				return c
			}
		}
		state[a] = done
		return ""
	}
	for _, p := range t.Permissions {
		if c := visit(p.Action); c != "" {
			return c
		}
	}
	return ""
}

// camelAction is the ref method name of an action (plugin_write is
// PluginWrite), for spotting a dimension set that would reuse it.
func camelAction(action Action) string {
	var b strings.Builder
	for _, word := range strings.Split(string(action), "_") {
		if word != "" {
			b.WriteString(strings.ToUpper(word[:1]) + word[1:])
		}
	}
	return b.String()
}
