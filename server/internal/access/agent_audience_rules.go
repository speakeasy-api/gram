package access

import (
	"slices"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Removing or narrowing a block restores access, unlike removing a grant.
// Require existing exclusions to survive replacement for inactive agents;
// additional blocks remain allowed without granting any new access.
func preserveUnassignableAgentBlocks(stored map[string]map[string]struct{}, assignable map[string]struct{}, entries []*gen.SetResourceAudienceEntry) error {
	proposed := make(map[string]map[string]struct{})
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		if proposed[entry.PrincipalUrn] == nil {
			proposed[entry.PrincipalUrn] = make(map[string]struct{})
		}
		proposed[entry.PrincipalUrn][audienceRuleKey(entry.Level, entry.Tools, entry.Dispositions)] = struct{}{}
	}
	for principal, rules := range stored {
		if _, active := assignable[principal]; active {
			continue
		}
		for rule := range rules {
			if !strings.HasPrefix(rule, "blocked") {
				continue
			}
			if _, retained := proposed[principal][rule]; !retained {
				return oops.E(oops.CodeInvalid, nil, "agent %q is suspended or revoked; its existing blocks must be preserved", principal)
			}
		}
	}
	return nil
}

// audienceRuleKey identifies one rule by everything that decides how much it
// grants: the level, and the narrowing that limits it. Order within the
// narrowing is not part of the rule, so it is sorted out of the key.
func audienceRuleKey(level string, tools, dispositions []string) string {
	sortedTools := slices.Clone(tools)
	slices.Sort(sortedTools)
	sortedDispositions := slices.Clone(dispositions)
	slices.Sort(sortedDispositions)
	return level + "|" + strings.Join(sortedTools, ",") + "|" + strings.Join(sortedDispositions, ",")
}
