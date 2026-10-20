package assistants

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGoogleChatAdapter(t *testing.T) {
	t.Parallel()
	adapter, err := getSourceAdapter("googlechat")
	require.NoError(t, err)
	raw := []byte(`{"space_name":"spaces/test","space_type":"DIRECT_MESSAGE","thread_name":"spaces/test/threads/thread","message_name":"spaces/test/messages/msg","user_name":"users/test","text":"please help"}`)
	context, err := adapter.ThreadContext(raw)
	require.NoError(t, err)
	turn, err := adapter.DecodeTurn(assistantThreadEventRecord{NormalizedPayloadJSON: raw})
	require.NoError(t, err)
	for _, resource := range []string{"spaces/test", "spaces/test/threads/thread", "spaces/test/messages/msg", "users/test"} {
		require.Contains(t, context, resource)
		require.Contains(t, turn, resource)
	}
	require.Contains(t, turn, "please help")
	require.Contains(t, turn, "SpaceType: DIRECT_MESSAGE")
	guidance := adapter.OutputChannelGuidance()
	require.Contains(t, guidance, "Text responses are not delivered")
	require.Contains(t, guidance, "No history is automatically fetched")
	require.Contains(t, guidance, "only on demand")
	require.Contains(t, guidance, "DIRECT_MESSAGE (legacy DM) or GROUP_CHAT: use platform_googlechat_send_message")
	require.Contains(t, guidance, "Named SPACE (legacy ROOM): use platform_googlechat_reply_to_thread")
	require.Contains(t, guidance, "reuse it unchanged on every retry")
}
