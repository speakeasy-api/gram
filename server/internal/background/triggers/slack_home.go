package triggers

import (
	"context"
	"encoding/json"

	"github.com/speakeasy-api/gram/server/internal/attr"
	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

// slackAppHomeOpenedEventBody matches app_home_opened. Slack fires it for
// both the Home and Messages tabs; tab tells them apart.
// See https://docs.slack.dev/reference/events/app_home_opened.
type slackAppHomeOpenedEventBody struct {
	Type string `json:"type"`
	User string `json:"user"`
	Tab  string `json:"tab"`
}

// publishSlackAppHome answers a Slack app_home_opened event for the Home tab
// with a views.publish call. Slack keeps the Home tab spinning until the app
// publishes a view for the user, so this runs for every opening regardless of
// the trigger's event_types subscription. Best-effort, mirroring
// unfurlSlackGramLinks: a failure here only costs the Home tab content.
func (a *App) publishSlackAppHome(ctx context.Context, instance triggerrepo.TriggerInstance, env map[string]string, body []byte, event EventEnvelope) {
	if a.slackClient == nil || a.siteURL == nil || !isSlackDefinition(instance.DefinitionSlug) {
		return
	}
	if instance.Status != StatusActive {
		return
	}
	evt, isSlack := event.Event.(slackTriggerEvent)
	if !isSlack || evt.EventType != "app_home_opened" {
		return
	}
	token := toolconfig.CIEnvFrom(env).Get("SLACK_BOT_TOKEN")
	if token == "" {
		return
	}

	// The normalized event drops the tab, so recover it from the raw body.
	var req slackEventRequest
	if err := json.Unmarshal(body, &req); err != nil || len(req.Event) == 0 {
		return
	}
	var homeEvent slackAppHomeOpenedEventBody
	if err := json.Unmarshal(req.Event, &homeEvent); err != nil {
		return
	}
	if homeEvent.Tab != "home" || homeEvent.User == "" {
		return
	}

	if err := a.slackClient.PublishView(ctx, token, slackclient.SlackPublishViewInput{
		UserID: homeEvent.User,
		View:   SlackAppHomeView(a.siteURL.String()),
	}); err != nil {
		a.logger.WarnContext(ctx, "publish slack app home", attr.SlogError(err))
	}
}

// SlackAppHomeView builds the Home tab: a welcome message and a button
// linking to the Speakeasy dashboard.
func SlackAppHomeView(dashboardURL string) map[string]any {
	return map[string]any{
		"type": "home",
		"blocks": []any{
			map[string]any{
				"type": "header",
				"text": map[string]any{"type": "plain_text", "text": "Welcome to Speakeasy"},
			},
			map[string]any{
				"type": "section",
				"text": map[string]any{
					"type": "mrkdwn",
					"text": "Mention me in a channel or send me a direct message from the *Messages* tab to get started.",
				},
			},
			map[string]any{
				"type": "actions",
				"elements": []any{
					map[string]any{
						"type":      "button",
						"action_id": "open_dashboard",
						"text":      map[string]any{"type": "plain_text", "text": "Open the Speakeasy dashboard"},
						"url":       dashboardURL,
						"style":     "primary",
					},
				},
			},
		},
	}
}
