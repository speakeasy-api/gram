package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetChannelInfoTool_PostsToConversationsInfo(t *testing.T) {
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
		descriptor: NewGetChannelInfoTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callGetChannelInfo,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"include_locale":true,
		"include_num_members":true
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/conversations.info", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "true", requestPayload.Get("include_locale"))
	require.Equal(t, "true", requestPayload.Get("include_num_members"))
	require.JSONEq(t, `{"ok":true,"channel":{"id":"C123"}}`, out.String())
}
