package authz

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Selector is a set of key-value constraints attached to a grant or check.
// Grants use explicit resource_kind and resource_id keys, e.g.
// {"resource_kind":"project","resource_id":"proj_123"}.
// Wildcard uses {"resource_kind":"*","resource_id":"*"}.
// For a grant selector to match a check selector, every key in the grant must
// either equal the corresponding check value or be the wildcard "*".
type Selector map[string]string

// Matches reports whether this (grant) selector satisfies the given check
// selector. A nil/empty grant selector matches any check (defensive fallback).
// For each key present in BOTH the grant and check selectors, the values must
// be equal or the grant value must be "*". Keys present in the grant but absent
// from the check are skipped — the check is not constraining that dimension.
// This allows disposition-scoped grants (e.g. {"disposition":"read_only"}) to
// match connection-level checks that don't yet specify a disposition.
func (s Selector) Matches(check Selector) bool {
	for k, grantVal := range s {
		checkVal, ok := check[k]
		if !ok {
			// Check doesn't constrain this dimension — skip.
			continue
		}
		if grantVal != "*" && grantVal != checkVal {
			return false
		}
	}
	return true
}

// ResourceKindForScope derives the resource kind from a scope's family prefix.
func ResourceKindForScope(scope Scope) string {
	s := string(scope)
	switch {
	case strings.HasPrefix(s, "project:"):
		return "project"
	case strings.HasPrefix(s, "remote-mcp:"):
		return "mcp"
	case strings.HasPrefix(s, "mcp:"):
		return "mcp"
	case strings.HasPrefix(s, "org:"):
		return "org"
	default:
		return "*"
	}
}

// Disposition values matching MCP tool annotation hint names (snake_case, no _hint suffix).
const (
	DispositionReadOnly    = "read_only"
	DispositionDestructive = "destructive"
	DispositionIdempotent  = "idempotent"
	DispositionOpenWorld   = "open_world"
)

// validDispositions is the set of allowed disposition values.
var validDispositions = map[string]bool{
	DispositionReadOnly:    true,
	DispositionDestructive: true,
	DispositionIdempotent:  true,
	DispositionOpenWorld:   true,
}

// allowedSelectorKeys defines which extra keys (beyond resource_kind and
// resource_id) are valid for each scope family. Scope families not listed here
// allow no extra keys.
var allowedSelectorKeys = map[string]map[string]bool{
	"mcp": {"tool": true, "disposition": true},
}

// ValidateSelector checks that a selector is well-formed for the given scope.
// Rules:
//   - resource_kind and resource_id must both be present
//   - resource_kind must match the scope family (or be "*" for root)
//   - extra keys must be in the allowed set for the scope family
//   - unknown keys are rejected
func ValidateSelector(scope Scope, sel Selector) error {
	kind, hasKind := sel["resource_kind"]
	_, hasID := sel["resource_id"]
	if !hasKind || !hasID {
		return fmt.Errorf("selector must include both resource_kind and resource_id")
	}

	expectedKind := ResourceKindForScope(scope)
	if scope == ScopeRoot {
		if kind != "*" {
			return fmt.Errorf("root scope requires resource_kind=*, got %q", kind)
		}
		// root allows no extra keys
		for k := range sel {
			if k != "resource_kind" && k != "resource_id" {
				return fmt.Errorf("root scope does not allow extra selector key %q", k)
			}
		}
		return nil
	}

	if kind != expectedKind {
		return fmt.Errorf("scope %q requires resource_kind=%q, got %q", scope, expectedKind, kind)
	}

	allowed := allowedSelectorKeys[expectedKind]
	for k, v := range sel {
		if k == "resource_kind" || k == "resource_id" {
			continue
		}
		if !allowed[k] {
			return fmt.Errorf("selector key %q is not allowed for scope %q", k, scope)
		}
		if k == "disposition" && !validDispositions[v] {
			return fmt.Errorf("invalid disposition value %q", v)
		}
	}

	return nil
}

// NewSelector creates a selector with resource_kind derived from scope.
func NewSelector(scope Scope, resourceID string) Selector {
	return Selector{
		"resource_kind": ResourceKindForScope(scope),
		"resource_id":   resourceID,
	}
}

// NewGrant creates a Grant with selector derived from scope and resource ID.
func NewGrant(scope Scope, resourceID string) Grant {
	return Grant{
		Scope:    scope,
		Selector: NewSelector(scope, resourceID),
	}
}

// NewGrantWithSelector creates a Grant with an explicit selector.
func NewGrantWithSelector(scope Scope, selector Selector) Grant {
	return Grant{
		Scope:    scope,
		Selector: selector,
	}
}

// ResourceID extracts the resource_id value from the selector.
// Returns "*" if no resource_id key is present.
func (s Selector) ResourceID() string {
	if id, ok := s["resource_id"]; ok {
		return id
	}
	return WildcardResource
}

// SelectorFromRow parses the selectors JSONB column into a Selector.
func SelectorFromRow(selectors []byte) (Selector, error) {
	var sel Selector
	if err := json.Unmarshal(selectors, &sel); err != nil {
		return nil, fmt.Errorf("unmarshal selector: %w", err)
	}
	return sel, nil
}

// MarshalJSON implements json.Marshaler. A nil selector marshals as the
// explicit wildcard {"resource_kind":"*","resource_id":"*"}.
func (s Selector) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte(`{"resource_kind":"*","resource_id":"*"}`), nil
	}
	b, err := json.Marshal(map[string]string(s))
	if err != nil {
		return nil, fmt.Errorf("marshal selector: %w", err)
	}
	return b, nil
}
