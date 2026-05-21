package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetTeamInfoTool_PassesOptionalFields(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"team":{"id":"T123"}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewGetTeamInfoTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callGetTeamInfo,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{
		"team":"T123",
		"domain":"acme"
	}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/team.info", requestPath)
	require.Equal(t, "T123", requestPayload.Get("team"))
	require.Equal(t, "acme", requestPayload.Get("domain"))
}

func TestGetTeamInfoTool_AllowsEmptyPayload(t *testing.T) {
	t.Parallel()

	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPayload = readForm(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"team":{"id":"T123"}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewGetTeamInfoTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callGetTeamInfo,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), &bytes.Buffer{})
	require.NoError(t, err)
	require.Empty(t, requestPayload.Get("team"))
	require.Empty(t, requestPayload.Get("domain"))
}
