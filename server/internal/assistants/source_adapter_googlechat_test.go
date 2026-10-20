package assistants

import (
	"encoding/json"
	"fmt"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
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

func TestGoogleChatSourceContextPreservesSpaceType(t *testing.T) {
	t.Parallel()
	for _, spaceType := range []string{"DIRECT_MESSAGE", "GROUP_CHAT", "SPACE"} {
		t.Run(spaceType, func(t *testing.T) {
			t.Parallel()
			raw := []byte(fmt.Sprintf(`{"space_name":"spaces/test","space_type":%q,"thread_name":"spaces/test/threads/thread","message_name":"spaces/test/messages/message","user_name":"users/test","text":"hello"}`, spaceType))
			kind, sourceRef, normalized, source, err := buildAssistantEventPayload(bgtriggers.Task{DefinitionSlug: sourceKindGoogleChat, EventJSON: raw, RawPayload: raw})
			require.NoError(t, err)
			require.Equal(t, sourceKindGoogleChat, kind)
			require.JSONEq(t, string(raw), string(normalized))
			require.JSONEq(t, string(raw), string(source))
			var ref googlechatSourceRef
			require.NoError(t, json.Unmarshal(sourceRef, &ref))
			require.Equal(t, spaceType, ref.SpaceType)
			require.Equal(t, "spaces/test", ref.SpaceName)
			require.Equal(t, "spaces/test/threads/thread", ref.ThreadName)
			adapter, err := getSourceAdapter(kind)
			require.NoError(t, err)
			// Auth continuation uses persisted thread context without DecodeTurn.
			context, err := adapter.ThreadContext(sourceRef)
			require.NoError(t, err)
			require.Contains(t, context, "SpaceType: "+spaceType+"\n")
			require.Contains(t, context, "SpaceName: spaces/test\n")
			require.Contains(t, context, "ThreadName: spaces/test/threads/thread\n")
		})
	}
}
