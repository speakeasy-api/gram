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

func TestEditCanvasTool_SendsChanges(t *testing.T) {
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
		descriptor: NewEditCanvasTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callEditCanvas,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"canvas_id":"F1",
		"changes":[
			{"operation":"insert_at_end","document_content":{"type":"markdown","markdown":"Appendix"}},
			{"operation":"delete","section_id":"sec-2"}
		]
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/canvases.edit", requestPath)
	require.Equal(t, "F1", requestPayload.Get("canvas_id"))

	var changes []map[string]any
	require.NoError(t, json.Unmarshal([]byte(requestPayload.Get("changes")), &changes))
	require.Len(t, changes, 2)
	require.Equal(t, "insert_at_end", changes[0]["operation"])
	doc, ok := changes[0]["document_content"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Appendix", doc["markdown"])
	require.Equal(t, "delete", changes[1]["operation"])
	require.Equal(t, "sec-2", changes[1]["section_id"])
}

func TestEditCanvasTool_RequiresCanvasID(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewEditCanvasTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callEditCanvas,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"changes":[{"operation":"insert_at_end"}]}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, "canvas_id")
}

func TestEditCanvasTool_RequiresChanges(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewEditCanvasTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callEditCanvas,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"canvas_id":"F1"}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, "changes")
}
