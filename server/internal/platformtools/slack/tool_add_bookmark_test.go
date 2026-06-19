package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddBookmarkTool_PassesOptionalFields(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"bookmark":{"id":"Bk1"}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewAddBookmarkTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callAddBookmark,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C123",
		"title":"Docs",
		"type":"link",
		"link":"https://example.com",
		"emoji":":memo:",
		"entity_id":"E123",
		"parent_id":"Bk0"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/bookmarks.add", requestPath)
	require.Equal(t, "C123", requestPayload.Get("channel_id"))
	require.Equal(t, "Docs", requestPayload.Get("title"))
	require.Equal(t, "link", requestPayload.Get("type"))
	require.Equal(t, "https://example.com", requestPayload.Get("link"))
	require.Equal(t, ":memo:", requestPayload.Get("emoji"))
	require.Equal(t, "E123", requestPayload.Get("entity_id"))
	require.Equal(t, "Bk0", requestPayload.Get("parent_id"))
	require.JSONEq(t, `{"ok":true,"bookmark":{"id":"Bk1"}}`, out.String())
}

func TestAddBookmarkTool_RequiresFields(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewAddBookmarkTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callAddBookmark,
	}

	cases := []struct {
		name    string
		payload string
		field   string
	}{
		{"missing channel", `{"title":"T","type":"link","link":"https://x"}`, "channel_id"},
		{"missing title", `{"channel_id":"C","type":"link","link":"https://x"}`, "title"},
		{"missing type", `{"channel_id":"C","title":"T","link":"https://x"}`, "type"},
		{"missing link", `{"channel_id":"C","title":"T","type":"link"}`, "link"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(tc.payload), &bytes.Buffer{})
			require.Error(t, err)
			require.ErrorContains(t, err, tc.field)
		})
	}
}
