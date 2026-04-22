package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadChannelMessagesTool_UsesSlackTokenFromEnv(t *testing.T) {
	t.Parallel()

	var authorization string
	var contentType string
	var requestPath string
	var requestPayload url.Values

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		contentType = r.Header.Get("Content-Type")
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"messages":[{"ts":"123.456","text":"hello"}]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewReadChannelMessagesTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callReadChannelMessages,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"channel_id":"C123","limit":25}`), &out)
	require.NoError(t, err)

	require.Equal(t, "Bearer xoxb-test-token", authorization)
	require.Equal(t, "application/x-www-form-urlencoded", contentType)
	require.Equal(t, "/conversations.history", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel"))
	require.Equal(t, "25", requestPayload.Get("limit"))
	require.JSONEq(t, `{"ok":true,"messages":[{"ts":"123.456","text":"hello"}]}`, out.String())
}
