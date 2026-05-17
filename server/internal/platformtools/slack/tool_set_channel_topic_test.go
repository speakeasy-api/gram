package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetChannelTopicTool_PostsToConversationsSetTopic(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"topic":"welcome"}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSetChannelTopicTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSetChannelTopic,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"topic":"welcome"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/conversations.setTopic", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "welcome", requestPayload.Get("topic"))
}
