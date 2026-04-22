package access

import (
	"encoding/json"
	"fmt"
)

// Selector is a set of key-value constraints attached to a grant or check.
// An empty selector matches everything (wildcard). For a grant selector to
// match a check selector, every key in the grant must either equal the
// corresponding check value or be the wildcard "*".
type Selector map[string]string

// Matches reports whether this (grant) selector satisfies the given check
// selector. An empty grant selector matches any check. For each key in the
// grant selector, the check must contain the same key with either an equal
// value or the grant value must be "*".
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
// "*" maps to an empty selector (wildcard); any other value maps to
// {"resource_id": id}.
func ForResource(resourceID string) Selector {
	if resourceID == WildcardResource {
		return Selector{}
	}
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

// MarshalJSON implements json.Marshaler. A nil selector marshals as "{}".
func (s Selector) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(map[string]string(s))
	if err != nil {
		return nil, fmt.Errorf("marshal selector: %w", err)
	}
	return b, nil
}
