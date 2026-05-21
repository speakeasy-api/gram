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

func TestDeleteReminderTool_CallsRemindersDeleteWithUserToken(t *testing.T) {
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
		descriptor: NewDeleteReminderTool(nil).Descriptor(),
		client:     newAPIClient(server.URL, server.Client()),
		callFn:     callDeleteReminder,
	}

	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.CIEnvFrom(map[string]string{slackUserTokenEnvVar: "xoxp-user-token"}),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"reminder":"Rm12345678","team_id":"T999"}`), io.Discard)
	require.NoError(t, err)
	require.Equal(t, "/reminders.delete", requestPath)
	require.Equal(t, "Rm12345678", requestPayload.Get("reminder"))
	require.Equal(t, "T999", requestPayload.Get("team_id"))
}
