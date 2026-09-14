package access

import (
	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPreserveUnassignableAgentBlocks(t *testing.T) {
	t.Parallel()
	const principal = "agent:example"
	stored := map[string]map[string]struct{}{principal: {audienceRuleKey("blocked", nil, nil): {}}}
	for _, level := range []string{"blocked", "blocked_view", "blocked_manage"} {
		stored[principal] = map[string]struct{}{audienceRuleKey(level, nil, nil): {}}
		require.Error(t, preserveUnassignableAgentBlocks(stored, nil, nil))
		require.Error(t, preserveUnassignableAgentBlocks(stored, nil, []*gen.SetResourceAudienceEntry{{PrincipalUrn: principal, Level: level, Tools: []string{"search"}}}))
		require.Error(t, preserveUnassignableAgentBlocks(stored, nil, []*gen.SetResourceAudienceEntry{{PrincipalUrn: principal, Level: level, Dispositions: []string{"destructive"}}}))
		require.NoError(t, preserveUnassignableAgentBlocks(stored, nil, []*gen.SetResourceAudienceEntry{{PrincipalUrn: principal, Level: level}}))
	}
	require.NoError(t, preserveUnassignableAgentBlocks(stored, map[string]struct{}{principal: {}}, nil))
	require.NoError(t, preserveUnassignableAgentBlocks(nil, nil, []*gen.SetResourceAudienceEntry{{PrincipalUrn: principal, Level: "blocked"}}))
	stored[principal] = map[string]struct{}{audienceRuleKey("use", nil, nil): {}}
	require.NoError(t, preserveUnassignableAgentBlocks(stored, nil, nil))
}
