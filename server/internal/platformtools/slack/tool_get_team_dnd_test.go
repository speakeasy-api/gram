package slack

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetTeamDndTool_JoinsUserIDs(t *testing.T) {
	t.Parallel()

	var requestPath string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"users":{"U1":{"dnd_enabled":false},"U2":{"dnd_enabled":true}}}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewGetTeamDndTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callGetTeamDnd,
	}

	var out bytes.Buffer
	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{"user_ids":["U1","U2"]}`), &out)
	require.NoError(t, err)

	require.Equal(t, "/dnd.teamInfo", requestPath)
	require.Equal(t, "U1,U2", requestPayload.Get("users"))
	require.Contains(t, out.String(), `"U2"`)
}

func TestGetTeamDndTool_RequiresUserIDs(t *testing.T) {
	t.Parallel()

	tool := &slackTool{
		descriptor: NewGetTeamDndTool(nil).Descriptor(),
		client:     newAPIClient("https://slack.test.invalid", nil),
		callFn:     callGetTeamDnd,
	}

	err := tool.Call(t.Context(), testSlackEnv(), bytes.NewBufferString(`{}`), &bytes.Buffer{})
	require.Error(t, err)
	require.ErrorContains(t, err, "user_ids")
}
