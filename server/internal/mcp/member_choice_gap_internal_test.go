package mcp

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// Only a member with no recorded choice links its connect page; a gap an
// administrator must fix keeps its message.
func TestMemberChoiceGapLinksOnlyMissingChoices(t *testing.T) {
	t.Parallel()
	s := &Service{}
	const link = "https://example.test/mcp/gw/connect/first-party?server=member"

	missing := metaMember{slug: "member"}
	s.applyMemberAuthClient(t.Context(), &missing, gapResolution(authClientReasonMissingChoice), nil)
	linked := withMemberConnectLink(missing.upstreamUnavailable, missing, link)
	require.Contains(t, linked.Error(), link)
	_, credential := errors.AsType[memberCredentialError](linked)
	require.True(t, credential)

	for _, reason := range []string{authClientReasonClientDisabled, authClientReasonClientDeleted, authClientReasonNotPermitted, authClientReasonSetup, authClientReasonAmbiguous} {
		member := metaMember{slug: "member"}
		s.applyMemberAuthClient(t.Context(), &member, gapResolution(reason), nil)
		unlinked := withMemberConnectLink(member.upstreamUnavailable, member, link)
		require.NotContains(t, unlinked.Error(), link, reason)
		require.Equal(t, "This server's authentication needs setup or a new sign-in.", unlinked.Error(), reason)
	}
}
