package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostEphemeralTool_PostsToChatPostEphemeral(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"message_ts":"123.456"}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewChatPostEphemeralTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callPostEphemeral,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"user_id":"U987",
		"text":"hey only you",
		"thread_ts":"123.000",
		"link_names":true,
		"icon_emoji":":robot_face:",
		"username":"Gram"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/chat.postEphemeral", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "U987", requestPayload.Get("user"))
	require.Equal(t, "hey only you", requestPayload.Get("text"))
	require.Equal(t, "123.000", requestPayload.Get("thread_ts"))
	require.Equal(t, "true", requestPayload.Get("link_names"))
	require.Equal(t, ":robot_face:", requestPayload.Get("icon_emoji"))
	require.Equal(t, "Gram", requestPayload.Get("username"))
	require.JSONEq(t, `{"ok":true,"message_ts":"123.456"}`, out.String())
}

func TestPostEphemeralTool_RequiresContent(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewChatPostEphemeralTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callPostEphemeral,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"user_id":"U987"
	}`), &bytes.Buffer{})
	require.Error(t, err)
	require.ErrorContains(t, err, "text, blocks, or attachments")
}
