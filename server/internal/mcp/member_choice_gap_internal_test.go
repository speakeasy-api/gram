package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A gap other than a missing choice keeps its message and links nothing; the
// gateway tests cover a missing choice, which reads the server's setup.
func TestMemberChoiceGapLinksNothingForOtherGaps(t *testing.T) {
	t.Parallel()
	s := &Service{}
	const link = "https://example.test/mcp/gw/connect/first-party?server=member"

	for _, reason := range []string{authClientReasonClientDisabled, authClientReasonClientDeleted, authClientReasonNotPermitted, authClientReasonSetup, authClientReasonAmbiguous} {
		member := metaMember{slug: "member"}
		s.applyMemberAuthClient(t.Context(), "org", &member, gapResolution(reason), nil)
		unlinked := withMemberConnectLink(member.upstreamUnavailable, member, link)
		require.NotContains(t, unlinked.Error(), link, reason)
		require.Equal(t, "This server's authentication needs setup or a new sign-in.", unlinked.Error(), reason)
	}
}
