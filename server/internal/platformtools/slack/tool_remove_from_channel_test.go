package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoveFromChannelTool_PostsToConversationsKick(t *testing.T) {
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
		descriptor: NewRemoveFromChannelTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callRemoveFromChannel,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"user_id":"U1"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/conversations.kick", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "U1", requestPayload.Get("user"))
}
