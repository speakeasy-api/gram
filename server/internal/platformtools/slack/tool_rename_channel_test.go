package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameChannelTool_PostsToConversationsRename(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"channel":{"id":"C123","name":"project-beta"}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewRenameChannelTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callRenameChannel,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"name":"project-beta"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/conversations.rename", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "project-beta", requestPayload.Get("name"))
}
