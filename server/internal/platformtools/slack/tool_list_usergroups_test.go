package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListUsergroupsTool_PassesOptionalFields(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"usergroups":[]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListUsergroupsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListUsergroups,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"include_count":true,
		"include_disabled":false,
		"include_users":true,
		"team_id":"T123"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/usergroups.list", requestPath)
	require.Equal(t, "true", requestPayload.Get("include_count"))
	require.Equal(t, "false", requestPayload.Get("include_disabled"))
	require.Equal(t, "true", requestPayload.Get("include_users"))
	require.Equal(t, "T123", requestPayload.Get("team_id"))
}

func TestListUsergroupsTool_AllowsEmptyPayload(t *testing.T) {
	t.Parallel()

	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPayload = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"usergroups":[]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListUsergroupsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListUsergroups,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), &bytes.Buffer{})
	require.NoError(t, err)
	require.Empty(t, requestPayload.Get("include_count"))
	require.Empty(t, requestPayload.Get("team_id"))
}
