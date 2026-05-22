package slack

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateChannelCanvasTool_PassesOptionalFields(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"canvas_id":"F1"}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewCreateChannelCanvasTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callCreateChannelCanvas,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"channel_id":"C100",
		"title":"Channel doc",
		"document_content":{"type":"markdown","markdown":"body"}
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/conversations.canvases.create", requestPath)
	require.Equal(t, "C100", requestPayload.Get("channel_id"))
	require.Equal(t, "Channel doc", requestPayload.Get("title"))

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(requestPayload.Get("document_content")), &doc))
	require.Equal(t, "markdown", doc["type"])
	require.Equal(t, "body", doc["markdown"])
}

func TestCreateChannelCanvasTool_RequiresChannelID(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewCreateChannelCanvasTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callCreateChannelCanvas,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, "channel_id")
}
