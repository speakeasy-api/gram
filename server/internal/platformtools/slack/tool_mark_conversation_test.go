package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarkConversationTool_PostsToConversationsMark(t *testing.T) {
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
		descriptor: NewMarkConversationTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callMarkConversation,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"timestamp":"1234567890.123456"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/conversations.mark", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "1234567890.123456", requestPayload.Get("ts"))
}
