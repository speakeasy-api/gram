package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeleteMessageTool_PostsToChatDelete(t *testing.T) {
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
		descriptor: NewChatDeleteTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callDeleteMessage,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"ts":"123.456"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/chat.delete", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "123.456", requestPayload.Get("ts"))
	require.JSONEq(t, `{"ok":true,"channel":"C123","ts":"123.456"}`, out.String())
}
