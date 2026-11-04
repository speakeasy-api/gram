package mcp

import (
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAppendBuiltinCandidatesPreservesOrdinarySlugs(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	other := uuid.New()
	definition, ok := builtinmcp.Lookup(builtinmcp.Slack)
	require.True(t, ok)
	ordinary := []metaMemberCandidate{{projectID: project, slug: "builtin_slack"}, {projectID: project, slug: "builtin_slack_2"}}
	got := appendBuiltinCandidates(ordinary, []metaMemberCandidate{builtinCandidate(project, "", definition), builtinCandidate(other, "", definition)})
	require.Equal(t, "builtin_slack", got[0].slug)
	require.Equal(t, "builtin_slack_2", got[1].slug)
	require.Equal(t, "builtin_slack_3", got[2].slug)
	require.Equal(t, "builtin_slack", got[3].slug)
	members := []metaMember{{slug: got[0].slug, serverID: got[0].serverID}, {slug: got[2].slug, serverID: got[2].serverID}}
	builtin, found := findMetaMember(members, "builtin_slack_3")
	require.True(t, found)
	require.Equal(t, builtinmcp.ID(project, builtinmcp.Slack), builtin.serverID)
}
