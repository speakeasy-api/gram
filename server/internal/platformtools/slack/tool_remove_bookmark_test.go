package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoveBookmarkTool_PostsToBookmarksRemove(t *testing.T) {
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
		descriptor: NewRemoveBookmarkTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callRemoveBookmark,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"bookmark_id":"Bk1",
		"channel_id":"C123",
		"quip_section_id":"Q1"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/bookmarks.remove", requestPath)
	require.Equal(t, "Bk1", requestPayload.Get("bookmark_id"))
	require.Equal(t, "C123", requestPayload.Get("channel_id"))
	require.Equal(t, "Q1", requestPayload.Get("quip_section_id"))
}

func TestRemoveBookmarkTool_RequiresFields(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewRemoveBookmarkTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callRemoveBookmark,
	}

	cases := []struct {
		name    string
		payload string
		field   string
	}{
		{"missing bookmark", `{"channel_id":"C"}`, "bookmark_id"},
		{"missing channel", `{"bookmark_id":"Bk1"}`, "channel_id"},
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
