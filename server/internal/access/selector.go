package access

import (
	"encoding/json"
	"fmt"
)

// Selector is a set of key-value constraints attached to a grant or check.
// A wildcard grant uses {"resource_id": "*"} (explicit). For a grant selector
// to match a check selector, every key in the grant must either equal the
// corresponding check value or be the wildcard "*".
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

// ForResource converts a legacy resource ID to a Selector.
// "*" maps to {"resource_id": "*"} (explicit wildcard); any other value maps
// to {"resource_id": id}.
func ForResource(resourceID string) Selector {
	return Selector{"resource_id": resourceID}
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

// selectorFromRow parses the selectors JSONB column, falling back to the
// legacy resource string if selectors is NULL.
func selectorFromRow(selectors []byte, resource string) (Selector, error) {
	if len(selectors) > 0 {
		var sel Selector
		if err := json.Unmarshal(selectors, &sel); err != nil {
			return nil, fmt.Errorf("unmarshal selector: %w", err)
		}
		return sel, nil
	}
	return ForResource(resource), nil
}

// MarshalJSON implements json.Marshaler. A nil selector marshals as the
// explicit wildcard {"resource_id":"*"}.
func (s Selector) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte(`{"resource_id":"*"}`), nil
	}
	b, err := json.Marshal(map[string]string(s))
	if err != nil {
		return nil, fmt.Errorf("marshal selector: %w", err)
	}
	return b, nil
}
