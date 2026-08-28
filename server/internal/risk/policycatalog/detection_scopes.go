package policycatalog

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// EncodeDetectionScope converts a non-empty set of pinned message types into
// deterministic CEL. The caller remains responsible for compiling the result
// before persistence.
func EncodeDetectionScope(messageTypes []string, catalog Catalog) (string, error) {
	values := slices.Clone(messageTypes)
	slices.Sort(values)
	values = slices.Compact(values)
	if len(values) == 0 {
		return "", fmt.Errorf("detection scope message_types must not be empty")
	}
	for _, value := range values {
		if !slices.Contains(catalog.PolicyMessageTypes, value) {
			return "", fmt.Errorf("unsupported detection scope message type %q", value)
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode detection scope message types: %w", err)
	}
	return "kind in " + string(encoded), nil
}

// DecodeDetectionScope reverses only CEL emitted by EncodeDetectionScope.
func DecodeDetectionScope(scopeInclude, scopeExempt string, catalog Catalog) ([]string, bool) {
	if scopeExempt != "" || !strings.HasPrefix(scopeInclude, "kind in ") {
		return nil, false
	}
	var values []string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(scopeInclude, "kind in ")), &values); err != nil {
		return nil, false
	}
	canonical, err := EncodeDetectionScope(values, catalog)
	if err != nil || canonical != scopeInclude || len(values) != len(slices.Compact(slices.Clone(values))) {
		return nil, false
	}
	return values, true
}
