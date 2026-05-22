package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListScheduledMessagesTool_PostsToChatScheduledMessagesList(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"scheduled_messages":[]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewChatListScheduledMessagesTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListScheduledMessages,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"cursor":"dXNlcjpVMDYxTkZUVDI=",
		"latest":"1700000000",
		"oldest":"1600000000",
		"limit":50,
		"team_id":"T9999"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/chat.scheduledMessages.list", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "dXNlcjpVMDYxTkZUVDI=", requestPayload.Get("cursor"))
	require.Equal(t, "1700000000", requestPayload.Get("latest"))
	require.Equal(t, "1600000000", requestPayload.Get("oldest"))
	require.Equal(t, "50", requestPayload.Get("limit"))
	require.Equal(t, "T9999", requestPayload.Get("team_id"))
	require.JSONEq(t, `{"ok":true,"scheduled_messages":[]}`, out.String())
}
