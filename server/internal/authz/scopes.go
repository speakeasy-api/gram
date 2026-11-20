package authz

import (
	"cmp"
	"slices"
	"strings"
)

// Scope identifies an authorization capability granted on a resource.
type Scope string

type ScopeParts struct {
	Resource string
	Action   string
}

// The Scope* constants are generated from the registry into scopes_gen.go.

type scopeVisibility int

const (
	scopeVisibilityUserVisible scopeVisibility = iota + 1
	scopeVisibilityInternal
)

const (
	ScopeVisibilityUserVisible = "user_visible"
	ScopeVisibilityInternal    = "internal"
)

func (s Scope) Parts() ScopeParts {
	resource, action, ok := strings.Cut(string(s), ":")
	if !ok {
		return ScopeParts{Resource: string(s), Action: ""}
	}

	return ScopeParts{Resource: resource, Action: action}
}

// RegisteredScopes returns the scopes known to authorization in stable order.
func RegisteredScopes() []Scope {
	scopes := make([]Scope, 0, len(scopeVisibilityByScope))
	for scope := range scopeVisibilityByScope {
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)
	return scopes
}

func ScopeVisibilityFor(scope Scope) (string, bool) {
	visibility, ok := scopeVisibilityByScope[scope]
	if !ok {
		return "", false
	}

	switch visibility {
	case scopeVisibilityUserVisible:
		return ScopeVisibilityUserVisible, true
	case scopeVisibilityInternal:
		return ScopeVisibilityInternal, true
	default:
		return "", false
	}
}

// ExclusionScopeFor returns the scope that stores exception grants for the
// provided base scope.
func ExclusionScopeFor(scope Scope) (Scope, bool) {
	exclusion, ok := scopeExclusions[scope]
	return exclusion, ok && exclusion != ""
}

// scopeSubScopes is the user-visible inverse of scopeExpansions: for each
// higher-privilege scope, the lower scopes it implies (e.g.
// org:admin -> org:read). Internal blocklist expansions are intentionally not
// exposed as sub_scopes.
var scopeSubScopes map[Scope][]Scope

func init() {
	scopeSubScopes = make(map[Scope][]Scope)
	for lower, highers := range scopeExpansions {
		if scopeVisibilityByScope[lower] != scopeVisibilityUserVisible {
			continue
		}
		for _, h := range highers {
			if scopeVisibilityByScope[h] != scopeVisibilityUserVisible {
				continue
			}
			scopeSubScopes[h] = append(scopeSubScopes[h], lower)
		}
	}
	for _, lowers := range scopeSubScopes {
		slices.SortFunc(lowers, func(a, b Scope) int {
			return cmp.Compare(string(a), string(b))
		})
	}
}

func CalculateSubScopes(scope Scope) []string {
	lowers := scopeSubScopes[scope]
	out := make([]string, len(lowers))
	for i, s := range lowers {
		out[i] = string(s)
	}
	return out
}

// ScopeImplicationClosure returns the complete set of checks that a grant for
// scope can satisfy, including scope itself.
func ScopeImplicationClosure(scope Scope) []Scope {
	seen := make(map[Scope]struct{}, 1)
	seen[scope] = struct{}{}
	queue := []Scope{scope}
	for len(queue) > 0 {
		granted := queue[0]
		queue = queue[1:]
		for checked, satisfying := range scopeExpansions {
			if !slices.Contains(satisfying, granted) {
				continue
			}
			if _, ok := seen[checked]; ok {
				continue
			}
			seen[checked] = struct{}{}
			queue = append(queue, checked)
		}
	}

	closure := make([]Scope, 0, len(seen))
	for implied := range seen {
		closure = append(closure, implied)
	}
	slices.Sort(closure)
	return closure
}
