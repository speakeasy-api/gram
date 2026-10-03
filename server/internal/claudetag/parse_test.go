package claudetag_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/claudetag"
	"github.com/stretchr/testify/require"
)

func TestParseStandingOwner(t *testing.T) {
	t.Parallel()
	got := claudetag.Parse(`<standing_owner_message sender="U_DEMO_ONE" ts="1.2" originating-ask="true">hello &amp; welcome</standing_owner_message>
Delivery prose is not XML. <thread_activity><message author="U_DEMO_OTHER">history</message></thread_activity>`)
	require.True(t, got.Detected)
	require.Equal(t, "U_DEMO_ONE", got.Sender)
	require.Empty(t, got.ChildSession)
}

func TestParseHelperDeliveredToParent(t *testing.T) {
	t.Parallel()
	got := claudetag.Parse(`<cross-session-message from-session="session_demo_helper" standing-audience="parent" event-uuid="demo">Done.</cross-session-message>`)
	require.True(t, got.Detected)
	require.Equal(t, "session_demo_helper", got.ChildSession)
	require.Empty(t, got.Sender)
}

func TestParseRejectsReferenceOrMalformedEvidence(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		`Quoted example: <standing_owner_message sender="U_DEMO_ONE">hello</standing_owner_message>`,
		`<standing_owner_message sender="U_DEMO_ONE">unclosed`,
		`<standing_owner_message>missing sender</standing_owner_message>`,
		`<cross-session-message from-session="session_demo_other" standing-audience="peer">peer</cross-session-message>`,
		`<thread_activity><standing_owner_message sender="U_DEMO_ONE">history</standing_owner_message></thread_activity>`,
	} {
		require.False(t, claudetag.Parse(text).Detected, text)
	}
}

func TestParseContextWake(t *testing.T) {
	t.Parallel()
	got := claudetag.Parse("<session-context nonce=\"demo_nonce\">\nChannel: #demo-team (id: `C_DEMO`)\nWorkspace: `T_DEMO`\nYou: `@Claude` (bot user id `U_DEMO_BOT`)\n## Memory\nMarkdown with raw <@U_DEMO_BOT> and & signs.\n</session-context nonce=\"wrong_nonce\">\nStill context.\n</session-context nonce=\"demo_nonce\">\n" + `<wake reason="channel-activity"><channel id="C_DEMO" type="group"><message from="human" author-id="U_DEMO_ONE">hello &amp; welcome</message><message from="human" author-id="U_DEMO_BOT">joined</message><message from="sibling" author-id="B_DEMO_BOT">reply</message><message from="human" author-id="U_DEMO_TWO" trigger="true">summarize the demo rollout</message><message from="human" author-id="U_DEMO_ONE">again</message></channel></wake>`)
	require.True(t, got.Detected)
	require.Equal(t, "T_DEMO", got.Team)
	require.Equal(t, "C_DEMO", got.ChannelID)
	require.Equal(t, "demo-team", got.ChannelName)
	require.Equal(t, "Claude Tag in #demo-team", got.Title)
	require.Equal(t, []string{"U_DEMO_ONE", "U_DEMO_TWO"}, got.Senders)
	require.Equal(t, "summarize the demo rollout", got.Text)
}

func TestParseRejectsUnclosedContext(t *testing.T) {
	t.Parallel()
	require.False(t, claudetag.Parse(`<session-context nonce="demo"><standing_owner_message sender="U_DEMO">hello</standing_owner_message></session-context nonce="other">`).Detected)
	require.False(t, claudetag.Parse(`<session-context nonce="demo"><wake><channel id="C_DEMO"><message from="human">quoted</message></channel></wake></session-context nonce="demo">`).Detected)
}

func TestParseStandingOwnerChannelAttribute(t *testing.T) {
	t.Parallel()
	got := claudetag.Parse(`<standing_owner_message sender="U_DEMO_ONE" channel-id="C_DEMO">hello</standing_owner_message>`)
	require.True(t, got.Detected)
	require.Equal(t, "C_DEMO", got.ChannelID)
}

func TestParseWakeTitleSkipsEmptyHistory(t *testing.T) {
	t.Parallel()
	got := claudetag.Parse(`<wake><channel id="C_EMPTY" name="empty"><message from="human" author-id="U_DEMO_ONE"> </message></channel><channel id="C_TEXT" name="text"><message from="human" author-id="U_DEMO_TWO">hello</message></channel></wake>`)
	require.True(t, got.Detected)
	require.Equal(t, "C_TEXT", got.ChannelID)
	require.Equal(t, "Claude Tag in #text", got.Title)
}
