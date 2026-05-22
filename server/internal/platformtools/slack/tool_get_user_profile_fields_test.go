package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetUserProfileFieldsTool_PassesParams(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"profile":{"real_name":"Alice","fields":{"Xf01":{"value":"engineering","alt":""}}}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewGetUserProfileFieldsTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callGetUserProfileFields,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"user_id":"U123","include_labels":true}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/users.profile.get", requestPath)
	require.Equal(t, "U123", requestPayload.Get("user"))
	require.Equal(t, "true", requestPayload.Get("include_labels"))
	require.Contains(t, out.String(), `"fields"`)
}
