package toolsets

import (
	"errors"
	"strings"
)

func normalizeTopLevelToolUrns(topLevel, allowed []string) ([]string, error) {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, u := range allowed {
		allowedSet[u] = struct{}{}
	}

	out := make([]string, 0, len(topLevel))
	seen := make(map[string]struct{}, len(topLevel))
	for _, raw := range topLevel {
		u := strings.TrimSpace(raw)
		if u == "" {
			return nil, errors.New("top-level tool URN must not be empty")
		}
		if _, ok := allowedSet[u]; !ok {
			return nil, errors.New("top-level tool URN is not in this toolset")
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	return out, nil
}

func intersectToolUrns(topLevel, allowed []string) []string {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, u := range allowed {
		allowedSet[u] = struct{}{}
	}

	out := make([]string, 0, len(topLevel))
	seen := make(map[string]struct{}, len(topLevel))
	for _, u := range topLevel {
		if _, ok := allowedSet[u]; !ok {
			continue
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	return out
}
