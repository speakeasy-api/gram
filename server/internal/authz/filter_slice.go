package authz

import "context"

// FilterSlice builds one Check per item via checkFor, runs them through
// Engine.Filter, and returns the items whose checks passed, preserving input
// order. Items are matched back by their check's ResourceID — not by any id
// of their own — so items sharing a grant resource (e.g. two mcp_servers
// backed by the same toolset) are kept or dropped together.
func FilterSlice[T any](ctx context.Context, engine *Engine, items []T, checkFor func(T) Check) ([]T, error) {
	checks := make([]Check, len(items))
	for i, item := range items {
		checks[i] = checkFor(item)
	}

	allowedIDs, err := engine.Filter(ctx, checks)
	if err != nil {
		return nil, err
	}

	allowedSet := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allowedSet[id] = struct{}{}
	}

	allowed := make([]T, 0, len(allowedIDs))
	for i, item := range items {
		if _, ok := allowedSet[checks[i].ResourceID]; ok {
			allowed = append(allowed, item)
		}
	}

	return allowed, nil
}
