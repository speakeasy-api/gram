package access

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
// For each key in the grant selector, the check must contain the same key with
// either an equal value or the grant value must be "*".
func (s Selector) Matches(check Selector) bool {
	for k, grantVal := range s {
		checkVal, ok := check[k]
		if !ok {
			return false
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
	case strings.HasPrefix(s, "build:"):
		return "project"
	case strings.HasPrefix(s, "mcp:"):
		return "mcp"
	case strings.HasPrefix(s, "remote-mcp:"):
		return "mcp"
	case strings.HasPrefix(s, "org:"):
		return "org"
	default:
		return "*"
	}
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

// ResourceID extracts the resource_id value from the selector for backward
// compatibility with the API layer. Returns "*" if no resource_id key is
// present (i.e. the grant is unrestricted).
func (s Selector) ResourceID() string {
	if id, ok := s["resource_id"]; ok {
		return id
	}
	return WildcardResource
}

// selectorFromRow parses the selectors JSONB column, falling back to
// constructing a selector from the scope and resource if selectors is NULL.
func selectorFromRow(selectors []byte, scope Scope, resource string) (Selector, error) {
	if len(selectors) > 0 {
		var sel Selector
		if err := json.Unmarshal(selectors, &sel); err != nil {
			return nil, fmt.Errorf("unmarshal selector: %w", err)
		}
		return sel, nil
	}
	return NewSelector(scope, resource), nil
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
