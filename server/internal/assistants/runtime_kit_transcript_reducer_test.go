package assistants

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/assistants/kitacp"
	"github.com/stretchr/testify/require"
)

func TestKitTranscriptReducerChunkOnlyCompletionAndReplay(t *testing.T) {
	t.Parallel()
	notifications := []string{
		`{"sessionUpdate":"agent_message_chunk","messageId":"session-agent-1","content":{"type":"text","text":"final "}}`,
		`{"sessionUpdate":"agent_message_chunk","messageId":"session-agent-1","content":{"type":"text","text":"answer"}}`,
	}
	var final json.RawMessage
	for replay := range 2 {
		reducer := newKitTranscriptReducer()
		for _, raw := range notifications {
			n, err := reducer.Apply(kitacp.Notification{SessionID: "session", Method: "session/update", Params: json.RawMessage(`{"sessionId":"session","update":` + raw + `}`)})
			require.NoError(t, err)
			var envelope struct {
				Update kitTranscriptUpdate `json:"update"`
			}
			require.NoError(t, json.Unmarshal(n.Params, &envelope))
			require.Equal(t, "agent_message", envelope.Update.SessionUpdate)
			require.Equal(t, "session-agent-1", envelope.Update.MessageID)
			final = envelope.Update.Content
		}
		require.JSONEq(t, `[{"type":"text","text":"final answer"}]`, string(final), "replay %d must replace, not append to persisted content", replay)
	}
}

func TestKitTranscriptReducerReplacementAndClear(t *testing.T) {
	t.Parallel()
	reducer := newKitTranscriptReducer()
	for _, tc := range []struct{ update, want string }{
		{update: `{"sessionUpdate":"agent_message_chunk","messageId":"m","content":{"type":"text","text":"old"}}`, want: `[{"type":"text","text":"old"}]`},
		{update: `{"sessionUpdate":"agent_message","messageId":"m","content":[]}`, want: `[]`},
		{update: `{"sessionUpdate":"agent_message_chunk","messageId":"m","content":{"type":"text","text":"new"}}`, want: `[{"type":"text","text":"new"}]`},
		{update: `{"sessionUpdate":"agent_message","messageId":"m","content":null}`, want: `null`},
		{update: `{"sessionUpdate":"agent_message_chunk","messageId":"m","content":{"type":"image","data":"fixture","mimeType":"image/png"}}`, want: `[{"type":"image","data":"fixture","mimeType":"image/png"}]`},
	} {
		n, err := reducer.Apply(kitacp.Notification{SessionID: "session", Method: "session/update", Params: json.RawMessage(`{"sessionId":"session","update":` + tc.update + `}`)})
		require.NoError(t, err)
		var envelope struct {
			Update kitTranscriptUpdate `json:"update"`
		}
		require.NoError(t, json.Unmarshal(n.Params, &envelope))
		require.JSONEq(t, tc.want, string(envelope.Update.Content))
	}
}

func TestKitTranscriptReducerRetainedStateBounds(t *testing.T) {
	t.Parallel()
	r := newKitTranscriptReducer()
	for i := range kitTranscriptMaxMessages {
		require.NoError(t, r.replace(fmt.Sprint(i), []json.RawMessage{json.RawMessage(`{"type":"text","text":"x"}`)}))
	}
	require.ErrorContains(t, r.replace("overflow", nil), "message limit")
	require.Len(t, r.messages, kitTranscriptMaxMessages)
	require.NoError(t, r.replace("0", nil), "existing messages can still be cleared at the count limit")
	r = newKitTranscriptReducer()
	require.NoError(t, r.replace("session\x00m", []json.RawMessage{json.RawMessage(`{"type":"text","text":"x"}`)}))
	r.retainedBytes = kitTranscriptMaxRetainedBytes
	_, err := r.Apply(kitacp.Notification{Method: "session/update", Params: json.RawMessage(`{"sessionId":"session","update":{"sessionUpdate":"agent_message_chunk","messageId":"m","content":{"type":"text","text":"more"}}}`)})
	require.ErrorContains(t, err, "retained content limit")
	require.JSONEq(t, `{"type":"text","text":"x"}`, string(r.messages["session\x00m"][0]), "failed appends must not mutate retained blocks")
}

func TestKitTranscriptReducerTerminalReasonCorrelation(t *testing.T) {
	t.Parallel()
	r := newKitTranscriptReducer()
	apply := func(session, update string) kitTranscriptUpdate {
		n, err := r.Apply(kitacp.Notification{SessionID: session, Method: "session/update", Params: json.RawMessage(`{"sessionId":"` + session + `","update":` + update + `}`)})
		require.NoError(t, err)
		var envelope struct {
			Update kitTranscriptUpdate `json:"update"`
		}
		require.NoError(t, json.Unmarshal(n.Params, &envelope))
		return envelope.Update
	}
	apply("session", `{"sessionUpdate":"agent_message_chunk","messageId":"reply","content":{"type":"text","text":"hello"}}`)
	// A different session cannot terminate this session's message.
	other := apply("other", `{"sessionUpdate":"state_update","state":"idle","stopReason":"cancelled"}`)
	require.Empty(t, other.MessageID)
	terminal := apply("session", `{"sessionUpdate":"state_update","state":"idle","stopReason":"cancelled"}`)
	require.Equal(t, "reply", terminal.MessageID)
	require.Equal(t, "cancelled", terminal.StopReason)
	require.Equal(t, "state_update", terminal.SessionUpdate)
	// No-output turns must not attach their result to an earlier turn.
	terminal = apply("session", `{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}`)
	require.Empty(t, terminal.MessageID)
	apply("session", `{"sessionUpdate":"agent_message","messageId":"next","content":[]}`)
	terminal = apply("session", `{"sessionUpdate":"state_update","state":"idle"}`)
	require.Empty(t, terminal.StopReason)
	require.Empty(t, terminal.MessageID)
	terminal = apply("session", `{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}`)
	require.Empty(t, terminal.MessageID, "idle without a reason must still end correlation")
}
