package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeleteScheduledMessageTool_PostsToChatDeleteScheduledMessage(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewChatDeleteScheduledMessageTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callDeleteScheduledMessage,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"scheduled_message_id":"Q1298393284"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/chat.deleteScheduledMessage", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "Q1298393284", requestPayload.Get("scheduled_message_id"))
	require.JSONEq(t, `{"ok":true}`, out.String())
}
