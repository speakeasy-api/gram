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

// linkSharedWebhookBody is a Slack event_callback envelope carrying one Gram
// dashboard link and one foreign link, as delivered to the trigger webhook.
const linkSharedWebhookBody = `{
	"type": "event_callback",
	"team_id": "T024BE7LD",
	"event_id": "Ev123ABC456",
	"event": {
		"type": "link_shared",
		"channel": "C024BE91L",
		"user": "U0Z7K8SRH",
		"message_ts": "1593189506.000200",
		"unfurl_id": "C024BE91L.1593189506.000200.9dcb1",
		"source": "conversations_history",
		"links": [
			{"domain": "app.getgram.ai", "url": "https://app.getgram.ai/acme/projects/default/toolsets/my-tools"},
			{"domain": "example.com", "url": "https://example.com/not-ours"}
		]
	}
}`

func TestUnfurlSlackGramLinksCallsChatUnfurl(t *testing.T) {
	t.Parallel()

	var requestPath, authHeader string
	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	defer server.Close()

	siteURL, err := url.Parse("https://app.getgram.ai")
	require.NoError(t, err)
	app := &App{
		// testenv.NewLogger is unavailable here (import cycle back into this
		// package); the logger only fires on the warn path anyway.
		logger:      slog.New(slog.DiscardHandler), //nolint:forbidigo // GG006: testenv imports this package, so testenv.NewLogger(t) cannot be used here
		siteURL:     siteURL,
		slackClient: slackclient.NewSlackClientWithBaseURL(server.URL, server.Client()),
	}
	instance := triggerrepo.TriggerInstance{DefinitionSlug: DefinitionSlugSlack, Status: StatusActive}
	env := map[string]string{"SLACK_BOT_TOKEN": "xoxb-test-token"}
	envelope := EventEnvelope{Event: slackTriggerEvent{EventType: "link_shared"}}

	app.unfurlSlackGramLinks(t.Context(), instance, env, []byte(linkSharedWebhookBody), envelope)

	require.Equal(t, "/chat.unfurl", requestPath)
	require.Equal(t, "Bearer xoxb-test-token", authHeader)
	// The unfurl handle addresses the link, so channel/ts stay unset.
	require.Equal(t, "C024BE91L.1593189506.000200.9dcb1", form.Get("unfurl_id"))
	require.Equal(t, "conversations_history", form.Get("source"))
	require.Empty(t, form.Get("channel"))

	var unfurls map[string]struct {
		Blocks []struct {
			Type     string `json:"type"`
			Elements []struct {
				Type     string `json:"type"`
				ImageURL string `json:"image_url"`
				AltText  string `json:"alt_text"`
				Text     string `json:"text"`
			} `json:"elements"`
		} `json:"blocks"`
	}
	require.NoError(t, json.Unmarshal([]byte(form.Get("unfurls")), &unfurls))

	// Only the dashboard link is unfurled; the foreign domain is skipped.
	require.Len(t, unfurls, 1)
	preview, ok := unfurls["https://app.getgram.ai/acme/projects/default/toolsets/my-tools"]
	require.True(t, ok)
	require.Len(t, preview.Blocks, 1)
	require.Equal(t, "context", preview.Blocks[0].Type)
	require.Len(t, preview.Blocks[0].Elements, 2)
	require.Equal(t, "https://app.getgram.ai/favicon.png", preview.Blocks[0].Elements[0].ImageURL)
	require.Equal(t, "Speakeasy", preview.Blocks[0].Elements[0].AltText)
	require.Equal(t, "<https://app.getgram.ai/acme/projects/default/toolsets/my-tools|My Tools · Toolsets>", preview.Blocks[0].Elements[1].Text)
}

func TestUnfurlSlackGramLinksSkipsForeignLinksOnly(t *testing.T) {
	t.Parallel()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	siteURL, err := url.Parse("https://app.getgram.ai")
	require.NoError(t, err)
	app := &App{
		// testenv.NewLogger is unavailable here (import cycle back into this
		// package); the logger only fires on the warn path anyway.
		logger:      slog.New(slog.DiscardHandler), //nolint:forbidigo // GG006: testenv imports this package, so testenv.NewLogger(t) cannot be used here
		siteURL:     siteURL,
		slackClient: slackclient.NewSlackClientWithBaseURL(server.URL, server.Client()),
	}
	body := `{"type":"event_callback","event":{"type":"link_shared","channel":"C1","message_ts":"1.2","links":[{"domain":"example.com","url":"https://example.com/x"}]}}`

	app.unfurlSlackGramLinks(
		t.Context(),
		triggerrepo.TriggerInstance{DefinitionSlug: DefinitionSlugSlack, Status: StatusActive},
		map[string]string{"SLACK_BOT_TOKEN": "xoxb-test-token"},
		[]byte(body),
		EventEnvelope{Event: slackTriggerEvent{EventType: "link_shared"}},
	)

	require.False(t, called, "chat.unfurl must not be called when no dashboard links are shared")
}

func TestUnfurlSlackGramLinksSkipsPausedTriggers(t *testing.T) {
	t.Parallel()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	siteURL, err := url.Parse("https://app.getgram.ai")
	require.NoError(t, err)
	app := &App{
		// testenv.NewLogger is unavailable here (import cycle back into this
		// package); the logger only fires on the warn path anyway.
		logger:      slog.New(slog.DiscardHandler), //nolint:forbidigo // GG006: testenv imports this package, so testenv.NewLogger(t) cannot be used here
		siteURL:     siteURL,
		slackClient: slackclient.NewSlackClientWithBaseURL(server.URL, server.Client()),
	}

	app.unfurlSlackGramLinks(
		t.Context(),
		triggerrepo.TriggerInstance{DefinitionSlug: DefinitionSlugSlack, Status: StatusPaused},
		map[string]string{"SLACK_BOT_TOKEN": "xoxb-test-token"},
		[]byte(linkSharedWebhookBody),
		EventEnvelope{Event: slackTriggerEvent{EventType: "link_shared"}},
	)

	require.False(t, called, "chat.unfurl must not be called for a paused trigger")
}

func TestGramLinkTitleFromDashboardPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		link  string
		title string
	}{
		{"https://app.getgram.ai/acme/projects/default/toolsets/my-tools", "My Tools · Toolsets"},
		{"https://app.getgram.ai/acme/projects/default/toolsets", "Toolsets"},
		{"https://app.getgram.ai/acme/projects/default/chat/6f1e0b6a-9f2c-4d3e-8a5b-0c1d2e3f4a5b", "Chat"},
		{"https://app.getgram.ai/acme/projects/default/assistants/support_bot/settings", "Settings · Assistants"},
		{"https://app.getgram.ai/acme/settings", "Settings"},
		{"https://app.getgram.ai/acme", "Speakeasy dashboard"},
		{"https://app.getgram.ai/", "Speakeasy dashboard"},
		{"https://app.getgram.ai/acme/projects/default/logs/12345", "Logs"},
		// Separator-only segments humanize to nothing and must not produce
		// an empty label.
		{"https://app.getgram.ai/acme/projects/default/--/__", "Speakeasy dashboard"},
		{"https://app.getgram.ai/acme/projects/default/toolsets/--", "Toolsets"},
	}

	for _, tc := range cases {
		parsed, err := url.Parse(tc.link)
		require.NoError(t, err)
		require.Equal(t, tc.title, gramLinkTitle(parsed), "link %s", tc.link)
	}
}

func TestHumanizeSlugTitleCasesWords(t *testing.T) {
	t.Parallel()

	require.Equal(t, "My Cool Toolset", humanizeSlug("my-cool-toolset"))
	require.Equal(t, "Support Bot", humanizeSlug("support_bot"))
	require.Equal(t, "Chat", humanizeSlug("chat"))
}

func TestEscapeSlackTextEscapesControlCharacters(t *testing.T) {
	t.Parallel()

	require.Equal(t, "A &amp; B &lt;C&gt;", escapeSlackText("A & B <C>"))
}
