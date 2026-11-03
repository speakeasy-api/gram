package triggers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSlackEventSummaryAddressed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		summary SlackEventSummary
		want    bool
	}{
		{name: "mention", summary: SlackEventSummary{EventType: "app_mention", ChannelID: "C1", UserID: "U1"}, want: true},
		{name: "direct message", summary: SlackEventSummary{EventType: "message", ChannelID: "D1", UserID: "U1"}, want: true},
		{name: "direct message file share", summary: SlackEventSummary{EventType: "message", ChannelID: "D1", UserID: "U1", Subtype: "file_share"}, want: true},
		{name: "direct message thread broadcast", summary: SlackEventSummary{EventType: "message", ChannelID: "D1", UserID: "U1", Subtype: "thread_broadcast"}, want: true},
		{name: "direct message edit", summary: SlackEventSummary{EventType: "message", ChannelID: "D1", UserID: "U1", Subtype: "message_changed"}, want: false},
		{name: "channel message", summary: SlackEventSummary{EventType: "message", ChannelID: "C1", UserID: "U1"}, want: false},
		{name: "bot post", summary: SlackEventSummary{EventType: "message", ChannelID: "D1", UserID: "U1", BotID: "B1"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.summary.Addressed())
		})
	}
}
