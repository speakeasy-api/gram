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

func TestLookupCanvasSectionsTool_SendsCriteria(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"sections":[{"id":"sec-1"}]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewLookupCanvasSectionsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callLookupCanvasSections,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"canvas_id":"F1",
		"criteria":{"contains_text":"intro","section_types":["h1","h2"]}
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/canvases.sections.lookup", requestPath)
	require.Equal(t, "F1", requestPayload.Get("canvas_id"))

	var criteria map[string]any
	require.NoError(t, json.Unmarshal([]byte(requestPayload.Get("criteria")), &criteria))
	require.Equal(t, "intro", criteria["contains_text"])
	types, ok := criteria["section_types"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{"h1", "h2"}, types)
}

func TestLookupCanvasSectionsTool_RequiresCriteria(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewLookupCanvasSectionsTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callLookupCanvasSections,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"canvas_id":"F1","criteria":{}}`), io.Discard)
	require.Error(t, err)
	require.ErrorContains(t, err, "criteria")
}
