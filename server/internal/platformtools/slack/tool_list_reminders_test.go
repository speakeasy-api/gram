package slack

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

func TestListRemindersTool_CallsRemindersListWithUserToken(t *testing.T) {
	t.Parallel()

	var requestPath string
	var authorization string
	var requestPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		authorization = r.Header.Get("Authorization")
		requestPayload = readForm(t, r)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"reminders":[]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	tool := &slackTool{
		descriptor: NewListRemindersTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callListReminders,
	}

	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.CIEnvFrom(map[string]string{slackUserTokenEnvVar: "xoxp-user-token"}),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"team_id":"T123"}`), io.Discard)
	require.NoError(t, err)
	require.Equal(t, "/reminders.list", requestPath)
	require.Equal(t, "Bearer xoxp-user-token", authorization)
	require.Equal(t, "T123", requestPayload.Get("team_id"))
}
