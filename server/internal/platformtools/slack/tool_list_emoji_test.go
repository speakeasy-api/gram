package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListEmojiTool_PostsToEmojiList(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"emoji":{"shipit":"https://example.com/shipit.png"}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListEmojiTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListEmoji,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"include_categories":true}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/emoji.list", requestPath)
	require.Equal(t, "true", requestPayload.Get("include_categories"))
	require.Contains(t, out.String(), "shipit")
}

func TestListEmojiTool_OmitsUnsetFlag(t *testing.T) {
	t.Parallel()

	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPayload = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"emoji":{}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListEmojiTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListEmoji,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), &bytes.Buffer{})
	require.NoError(t, err)
	require.Empty(t, requestPayload.Get("include_categories"))
}
