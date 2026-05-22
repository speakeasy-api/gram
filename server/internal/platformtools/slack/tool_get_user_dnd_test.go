package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetUserDndTool_PassesUser(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"dnd_enabled":true,"next_dnd_start_ts":1700000000,"next_dnd_end_ts":1700003600}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewGetUserDndTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callGetUserDnd,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"user_id":"U123"}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/dnd.info", requestPath)
	require.Equal(t, "U123", requestPayload.Get("user"))
	require.Contains(t, out.String(), `"dnd_enabled":true`)
}
