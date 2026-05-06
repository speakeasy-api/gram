package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListReactionsTool_PassesOptionalFields(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"items":[]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListReactionsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListReactions,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"user_id":"U123",
		"full":true,
		"limit":50,
		"cursor":"abc"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/reactions.list", requestPath)
	require.Equal(t, "U123", requestPayload.Get("user"))
	require.Equal(t, "true", requestPayload.Get("full"))
	require.Equal(t, "50", requestPayload.Get("limit"))
	require.Equal(t, "abc", requestPayload.Get("cursor"))
}

func TestListReactionsTool_OmitsUnsetFields(t *testing.T) {
	t.Parallel()

	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPayload = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListReactionsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListReactions,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), &bytes.Buffer{})
	require.NoError(t, err)
	require.Empty(t, requestPayload.Get("user"))
	require.Empty(t, requestPayload.Get("full"))
	require.Empty(t, requestPayload.Get("limit"))
	require.Empty(t, requestPayload.Get("cursor"))
}
