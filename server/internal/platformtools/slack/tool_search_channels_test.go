package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchChannelsTool_ListsAndFiltersByQuery(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"channels":[
			{"id":"C1","name":"general"},
			{"id":"C2","name":"eng-platform"},
			{"id":"C3","name":"eng-runtime"}
		]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewSearchChannelsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callSearchChannels,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"query":"eng","channel_types":["public_channel","private_channel"],"exclude_archived":true}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/conversations.list", requestPath)
	require.Equal(t, "public_channel,private_channel", requestPayload.Get("types"))
	require.Equal(t, "true", requestPayload.Get("exclude_archived"))
	require.Contains(t, out.String(), "eng-platform")
	require.Contains(t, out.String(), "eng-runtime")
	require.NotContains(t, out.String(), "general")
}
