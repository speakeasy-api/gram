package access

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/stretchr/testify/require"
)

func TestPreserveUnassignableAgentBlocksMultipleRules(t *testing.T) {
	t.Parallel()
	const principal = "agent:example"
	const otherPrincipal = "agent:other"
	blockedSearch := &gen.SetResourceAudienceEntry{PrincipalUrn: principal, Level: "blocked", Tools: []string{"search"}}
	blockedDelete := &gen.SetResourceAudienceEntry{PrincipalUrn: principal, Level: "blocked_manage", Tools: []string{"delete"}}
	otherBlock := &gen.SetResourceAudienceEntry{PrincipalUrn: otherPrincipal, Level: "blocked_manage"}

	for _, tt := range []struct {
		name     string
		stored   []*gen.SetResourceAudienceEntry
		proposed []*gen.SetResourceAudienceEntry
		wantErr  bool
	}{
		{
			name:     "retaining only one of two rules fails",
			stored:   []*gen.SetResourceAudienceEntry{blockedSearch, blockedDelete},
			proposed: []*gen.SetResourceAudienceEntry{blockedSearch},
			wantErr:  true,
		},
		{
			name:     "dropping another principal's block fails",
			stored:   []*gen.SetResourceAudienceEntry{blockedSearch, otherBlock},
			proposed: []*gen.SetResourceAudienceEntry{blockedSearch},
			wantErr:  true,
		},
		{
			name:     "adding a block while retaining existing blocks succeeds",
			stored:   []*gen.SetResourceAudienceEntry{blockedSearch, otherBlock},
			proposed: []*gen.SetResourceAudienceEntry{blockedSearch, otherBlock, blockedDelete},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stored := make(map[string]map[string]struct{})
			for _, entry := range tt.stored {
				if stored[entry.PrincipalUrn] == nil {
					stored[entry.PrincipalUrn] = make(map[string]struct{})
				}
				stored[entry.PrincipalUrn][audienceRuleKey(entry.Level, entry.Tools, entry.Dispositions)] = struct{}{}
			}
			err := preserveUnassignableAgentBlocks(stored, nil, tt.proposed)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

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
