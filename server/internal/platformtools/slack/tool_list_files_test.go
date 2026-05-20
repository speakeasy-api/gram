package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListFilesTool_PassesAllFilters(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"files":[{"id":"F1"}]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListFilesTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListFiles,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"user":"U1",
		"channel":"C1",
		"ts_from":"1700000000.000000",
		"ts_to":"1700001000.000000",
		"types":"images,pdfs",
		"page":3,
		"count":50,
		"show_files_hidden_by_limit":true
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/files.list", requestPath)
	require.Equal(t, "U1", requestPayload.Get("user"))
	require.Equal(t, "C1", requestPayload.Get("channel"))
	require.Equal(t, "1700000000.000000", requestPayload.Get("ts_from"))
	require.Equal(t, "1700001000.000000", requestPayload.Get("ts_to"))
	require.Equal(t, "images,pdfs", requestPayload.Get("types"))
	require.Equal(t, "3", requestPayload.Get("page"))
	require.Equal(t, "50", requestPayload.Get("count"))
	require.Equal(t, "true", requestPayload.Get("show_files_hidden_by_limit"))
	require.JSONEq(t, `{"ok":true,"files":[{"id":"F1"}]}`, out.String())
}

func TestListFilesTool_OmitsUnsetOptionals(t *testing.T) {
	t.Parallel()

	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPayload = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"files":[]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListFilesTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListFiles,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), &bytes.Buffer{})
	require.NoError(t, err)
	require.Empty(t, requestPayload.Get("user"))
	require.Empty(t, requestPayload.Get("channel"))
	require.Empty(t, requestPayload.Get("page"))
}
