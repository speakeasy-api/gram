package assistants

import (
	"encoding/json"
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
