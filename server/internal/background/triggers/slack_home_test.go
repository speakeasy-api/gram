package triggers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func appHomeOpenedWebhookBody(tab string) []byte {
	return []byte(`{
	"type": "event_callback",
	"team_id": "T024BE7LD",
	"event_id": "Ev123ABC789",
	"event": {
		"type": "app_home_opened",
		"user": "U0Z7K8SRH",
		"channel": "D024BE91L",
		"tab": "` + tab + `",
		"event_ts": "1593189506.000200"
	}
}`)
}

func newSlackHomeTestApp(t *testing.T, handler http.HandlerFunc) *App {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	siteURL, err := url.Parse("https://app.getgram.ai")
	require.NoError(t, err)
	return &App{
		logger:      slog.New(slog.DiscardHandler), //nolint:forbidigo // GG006: testenv imports this package, so testenv.NewLogger(t) cannot be used here
		siteURL:     siteURL,
		slackClient: slackclient.NewSlackClientWithBaseURL(server.URL, server.Client()),
	}
}

func TestPublishSlackAppHomeCallsViewsPublish(t *testing.T) {
	t.Parallel()

	var requestPath, authHeader string
	var form url.Values
	app := newSlackHomeTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		authHeader = r.Header.Get("Authorization")
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	})
	instance := triggerrepo.TriggerInstance{DefinitionSlug: DefinitionSlugSlack, Status: StatusActive}
	env := map[string]string{"SLACK_BOT_TOKEN": "xoxb-test-token"}
	envelope := EventEnvelope{Event: slackTriggerEvent{EventType: "app_home_opened"}}

	app.publishSlackAppHome(t.Context(), instance, env, appHomeOpenedWebhookBody("home"), envelope)

	require.Equal(t, "/views.publish", requestPath)
	require.Equal(t, "Bearer xoxb-test-token", authHeader)
	require.Equal(t, "U0Z7K8SRH", form.Get("user_id"))

	var view struct {
		Type   string `json:"type"`
		Blocks []struct {
			Type     string `json:"type"`
			Elements []struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"elements"`
		} `json:"blocks"`
	}
	require.NoError(t, json.Unmarshal([]byte(form.Get("view")), &view))
	require.Equal(t, "home", view.Type)
	require.Len(t, view.Blocks, 3)
	require.Equal(t, "actions", view.Blocks[2].Type)
	require.Len(t, view.Blocks[2].Elements, 1)
	require.Equal(t, "button", view.Blocks[2].Elements[0].Type)
	require.Equal(t, "https://app.getgram.ai", view.Blocks[2].Elements[0].URL)
}

func TestPublishSlackAppHomeSkipsMessagesTab(t *testing.T) {
	t.Parallel()

	called := false
	app := newSlackHomeTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	instance := triggerrepo.TriggerInstance{DefinitionSlug: DefinitionSlugSlack, Status: StatusActive}
	env := map[string]string{"SLACK_BOT_TOKEN": "xoxb-test-token"}
	envelope := EventEnvelope{Event: slackTriggerEvent{EventType: "app_home_opened"}}

	app.publishSlackAppHome(t.Context(), instance, env, appHomeOpenedWebhookBody("messages"), envelope)

	require.False(t, called)
}
