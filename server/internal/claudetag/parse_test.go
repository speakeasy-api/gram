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

func TestParseHarnessFramingAndThreadWrappers(t *testing.T) {
	t.Parallel()
	got := claudetag.Parse(`<system-reminder source="memory">Opaque prose & <example>; <standing_owner_message sender="U_DEMO_QUOTED">quoted</standing_owner_message></system-reminder>
<session-context extra="future" nonce = 'demo_nonce'>
Channel: #demo-team (id: ` + "`C_DEMO`" + `)
Workspace: ` + "`T_DEMO`" + `
You: @Claude (bot user id ` + "`U_DEMO_BOT`" + `)
</session-context nonce='wrong_nonce'>
Still opaque.
</session-context nonce = 'demo_nonce'>
<system-reminder>More opaque & prose.</system-reminder>
<wake future="value"><channel id="C_DEMO"><participants><person slack-id="U_DEMO_OTHER"/></participants><message from="human" author-id="U_DEMO_ONE">first</message><thread ts="1"><messages><message from="human" sender="U_DEMO_TWO" trigger="true">Hi <@U_DEMO_BOT|Claude> & welcome</message><message from="human" slack-id="U_DEMO_BOT">bot</message><message from="sibling" author-id="U_DEMO_SIBLING">sibling</message></messages></thread><history><message from="human" author-id="U_DEMO_HISTORY">history</message></history><system-note><message from="human" author-id="U_DEMO_NOTE">note</message></system-note></channel></wake>
Trailing prose <not-xml`)
	require.True(t, got.Detected)
	require.Equal(t, "T_DEMO", got.Team)
	require.Equal(t, "C_DEMO", got.ChannelID)
	require.Equal(t, "demo-team", got.ChannelName)
	require.Equal(t, []string{"U_DEMO_ONE", "U_DEMO_TWO"}, got.Senders)
	require.Equal(t, "Hi <@U_DEMO_BOT|Claude> & welcome", got.Text)
}

func TestParseStandingOwnerAfterReminders(t *testing.T) {
	t.Parallel()
	got := claudetag.Parse(`<system-reminder>Instructions & <not-xml></system-reminder>
<system-reminder source='future'>More instructions.</system-reminder>
<standing_owner_message sender='U_DEMO_ONE' channel-id='C_DEMO' team-id='T_DEMO'>Hi <@U_DEMO_BOT> & welcome &amp; thanks</standing_owner_message>`)
	require.True(t, got.Detected)
	require.Equal(t, "U_DEMO_ONE", got.Sender)
	require.Equal(t, "C_DEMO", got.ChannelID)
	require.Equal(t, "T_DEMO", got.Team)
	require.Equal(t, "Hi <@U_DEMO_BOT> & welcome & thanks", got.Text)
}

func TestParseContextWithoutNonce(t *testing.T) {
	t.Parallel()
	got := claudetag.Parse(`<session-context>Workspace: ` + "`T_DEMO`" + `</session-context><wake><channel channel-id='C_DEMO'><thread><message from='human' slack-id='U_DEMO'>hello</message></thread></channel></wake>`)
	require.True(t, got.Detected)
	require.Equal(t, "T_DEMO", got.Team)
	require.Equal(t, "C_DEMO", got.ChannelID)
	require.Equal(t, []string{"U_DEMO"}, got.Senders)
}

func TestParseDoesNotSearchWithinFramingOrQuotedText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, text string }{
		{"invalid nonce", `<session-context nonce=broken>opaque</session-context><wake><channel id="C_DEMO"><message from="human">hello</message></channel></wake>`},
		{"framing name suffix", `<system-reminder-extra>opaque</system-reminder><wake><channel id="C_DEMO"><message from="human">hello</message></channel></wake>`},
		{"reminder only", `<system-reminder><standing_owner_message sender='U_DEMO'>quoted</standing_owner_message></system-reminder>`},
		{"unclosed reminder", `<system-reminder><standing_owner_message sender='U_DEMO'>quoted</standing_owner_message>`},
		{"prose after reminder", `<system-reminder>instructions</system-reminder>Example: <standing_owner_message sender='U_DEMO'>quoted</standing_owner_message>`},
		{"malformed thread", `<wake><channel id='C_DEMO'><thread><message from='human' author-id='U_DEMO'>hello</message></channel></wake>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.False(t, claudetag.Parse(tc.text).Detected)
		})
	}
}

func TestParsePreservesLiteralTextAndCDATA(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body, want string }{
		{"comparison", "a < b & c > d", "a < b & c > d"},
		{"CDATA closing tags", "<![CDATA[example </wake> </standing_owner_message> & raw text]]>", "example </wake> </standing_owner_message> & raw text"},
		{"CDATA", "<![CDATA[a < b & <@U_DEMO_BOT|Claude> &amp;]]>", "a < b & <@U_DEMO_BOT|Claude> &amp;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := claudetag.Parse(`<wake><channel id="C_DEMO"><message from="human" author-id="U_DEMO">` + tc.body + `</message></channel></wake>`)
			require.True(t, got.Detected)
			require.Equal(t, tc.want, got.Text)
			owner := claudetag.Parse(`<standing_owner_message sender="U_DEMO">` + tc.body + `</standing_owner_message>`)
			require.True(t, owner.Detected)
			require.Equal(t, tc.want, owner.Text)
		})
	}
}

func TestParseTrimsBotID(t *testing.T) {
	t.Parallel()
	got := claudetag.Parse("<session-context>You: @Claude (bot user id ` U_DEMO_BOT `)</session-context>" + `<wake><channel id="C_DEMO"><message from="human" author-id="U_DEMO_BOT">bot</message></channel></wake>`)
	require.False(t, got.Detected)
}

func TestParseContextNonceAmongFutureAttributes(t *testing.T) {
	t.Parallel()
	got := claudetag.Parse(`<session-context data-nonce="other" nonce="demo">opaque</session-context nonce="demo"><wake><channel id="C_DEMO"><message from="human">hello</message></channel></wake>`)
	require.True(t, got.Detected)
	require.Equal(t, "hello", got.Text)
}

func TestParseNamespacedContextNonce(t *testing.T) {
	t.Parallel()
	for _, nonce := range []string{"demo", "other"} {
		t.Run(nonce, func(t *testing.T) {
			t.Parallel()
			got := claudetag.Parse(`<session-context xmlns:h="urn:demo:harness" h:nonce="demo">opaque</session-context xmlns:h="urn:demo:harness" h:nonce="` + nonce + `"><wake><channel id="C_DEMO"><message from="human">hello</message></channel></wake>`)
			require.Equal(t, nonce == "demo", got.Detected)
		})
	}
}

func TestParseOpaqueReminderHeader(t *testing.T) {
	t.Parallel()
	got := claudetag.Parse(`<system-reminder source="a&b">opaque & <example></system-reminder><wake><channel id="C_DEMO"><message from="human">hello</message></channel></wake>`)
	require.True(t, got.Detected)
	require.Equal(t, "hello", got.Text)
}
