package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateChannelTool_PostsToConversationsCreate(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"channel":{"id":"C123"}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewCreateChannelTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callCreateChannel,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"name":"project-alpha",
		"is_private":true,
		"team_id":"T1"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/conversations.create", requestPath)
	require.Equal(t, "project-alpha", requestPayload.Get("name"))
	require.Equal(t, "true", requestPayload.Get("is_private"))
	require.Equal(t, "T1", requestPayload.Get("team_id"))
}
