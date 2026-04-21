package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSendMessageTool_PassesOptionalFields(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"channel":"C123","ts":"123.456"}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSendMessageTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSendMessage,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"text":"hello",
		"thread_ts":"123.000",
		"reply_broadcast":true,
		"unfurl_links":false,
		"unfurl_media":false
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/chat.postMessage", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "hello", requestPayload.Get("text"))
	require.Equal(t, "123.000", requestPayload.Get("thread_ts"))
	require.Equal(t, "true", requestPayload.Get("reply_broadcast"))
	require.Equal(t, "false", requestPayload.Get("unfurl_links"))
	require.Equal(t, "false", requestPayload.Get("unfurl_media"))
	require.JSONEq(t, `{"ok":true,"channel":"C123","ts":"123.456"}`, out.String())
}
