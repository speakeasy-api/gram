package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListUserConversationsTool_PassesParams(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"channels":[{"id":"C1","name":"general"}]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListUserConversationsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListUserConversations,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"user_id":"U99",
		"channel_types":["public_channel","im"],
		"exclude_archived":true,
		"limit":50,
		"cursor":"dXNlcjpVMQ=="
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/users.conversations", requestPath)
	require.Equal(t, "U99", requestPayload.Get("user"))
	require.Equal(t, "public_channel,im", requestPayload.Get("types"))
	require.Equal(t, "true", requestPayload.Get("exclude_archived"))
	require.Equal(t, "50", requestPayload.Get("limit"))
	require.Equal(t, "dXNlcjpVMQ==", requestPayload.Get("cursor"))
	require.Contains(t, out.String(), `"id":"C1"`)
}
