package slack

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateMessageTool_PostsToChatUpdate(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"channel":"C123","ts":"123.456","text":"updated"}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewChatUpdateTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callUpdateMessage,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"ts":"123.456",
		"text":"updated",
		"link_names":true,
		"parse":"full",
		"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"updated"}}]
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/chat.update", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "123.456", requestPayload.Get("ts"))
	require.Equal(t, "updated", requestPayload.Get("text"))
	require.Equal(t, "true", requestPayload.Get("link_names"))
	require.Equal(t, "full", requestPayload.Get("parse"))

	var blocks []map[string]any
	require.NoError(t, json.Unmarshal([]byte(requestPayload.Get("blocks")), &blocks))
	require.Len(t, blocks, 1)
	require.Equal(t, "section", blocks[0]["type"])
	require.JSONEq(t, `{"ok":true,"channel":"C123","ts":"123.456","text":"updated"}`, out.String())
}

func TestUpdateMessageTool_RequiresContent(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewChatUpdateTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callUpdateMessage,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"ts":"123.456"
	}`), &bytes.Buffer{})
	require.Error(t, err)
	require.ErrorContains(t, err, "text, blocks, or attachments")
}
