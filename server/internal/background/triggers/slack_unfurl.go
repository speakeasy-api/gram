package triggers

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

// maxSlackUnfurlLinks caps how many Gram links a single link_shared event can
// unfurl. Slack itself delivers at most a handful of links per event; the cap
// bounds the chat.unfurl payload if a message pastes many dashboard URLs.
const maxSlackUnfurlLinks = 10

// unfurlSlackGramLinks answers a Slack link_shared event with a chat.unfurl
// call for every shared link that points at the Gram dashboard, attaching the
// Gram logo and a title derived from the URL path. Best-effort, mirroring
// ackSlackThreadStatus: a failure here only costs the link preview.
//
// Titles are computed purely from the URL (humanized path slugs) — never from
// database lookups — so an unfurl reveals nothing beyond what the pasted URL
// already shows to everyone in the channel.
func (a *App) unfurlSlackGramLinks(ctx context.Context, instance triggerrepo.TriggerInstance, env map[string]string, body []byte, event EventEnvelope) {
	if a.slackClient == nil || a.siteURL == nil || instance.DefinitionSlug != DefinitionSlugSlack {
		return
	}
	evt, isSlack := event.Event.(slackTriggerEvent)
	if !isSlack || evt.EventType != "link_shared" {
		return
	}
	token := toolconfig.CIEnvFrom(env).Get("SLACK_BOT_TOKEN")
	if token == "" {
		return
	}

	// slackIngest normalizes away the links array, so recover it from the raw
	// webhook body rather than widening the CEL-visible event shape.
	var req slackEventRequest
	if err := json.Unmarshal(body, &req); err != nil || len(req.Event) == 0 {
		return
	}
	var linkEvent slackLinkSharedEventBody
	if err := json.Unmarshal(req.Event, &linkEvent); err != nil {
		return
	}

	unfurls := make(map[string]any)
	iconURL := a.siteURL.JoinPath("favicon.png").String()
	for _, link := range linkEvent.Links {
		if len(unfurls) >= maxSlackUnfurlLinks {
			break
		}
		parsed, err := url.Parse(link.URL)
		if err != nil || !strings.EqualFold(parsed.Hostname(), a.siteURL.Hostname()) {
			continue
		}
		unfurls[link.URL] = map[string]any{
			"blocks": []any{
				map[string]any{
					"type": "context",
					"elements": []any{
						map[string]any{"type": "image", "image_url": iconURL, "alt_text": "Gram"},
						map[string]any{"type": "mrkdwn", "text": "<" + link.URL + "|" + escapeSlackText(gramLinkTitle(parsed)) + ">"},
					},
				},
			},
		}
	}
	if len(unfurls) == 0 {
		return
	}

	if err := a.slackClient.Unfurl(ctx, token, slackclient.SlackUnfurlInput{
		ChannelID: linkEvent.Channel,
		MessageTS: linkEvent.MessageTs,
		UnfurlID:  linkEvent.UnfurlID,
		Source:    linkEvent.Source,
		Unfurls:   unfurls,
	}); err != nil {
		a.logger.WarnContext(ctx, "unfurl slack gram links", attr.SlogError(err))
	}
}

// opaqueSlugPattern matches path segments that carry no human meaning: UUIDs,
// long hex identifiers, and purely numeric ids.
var opaqueSlugPattern = regexp.MustCompile(`^(?i:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9a-f]{16,}|[0-9]+)$`)

// gramLinkTitle derives a human-readable unfurl title from a dashboard URL
// path. Dashboard routes look like /{orgSlug}[/projects/{projectSlug}]/...,
// so the org and project prefix is stripped and the remaining slugs are
// humanized: /acme/projects/default/toolsets/my-tools → "My Tools · Toolsets".
// Opaque segments (UUIDs, numeric ids) are skipped so a chat URL falls back
// to its section name instead of a raw identifier.
func gramLinkTitle(u *url.URL) string {
	segments := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	if len(segments) > 0 {
		segments = segments[1:]
	}
	if len(segments) >= 2 && segments[0] == "projects" {
		segments = segments[2:]
	}

	meaningful := make([]string, 0, len(segments))
	for _, segment := range segments {
		if !opaqueSlugPattern.MatchString(segment) {
			meaningful = append(meaningful, segment)
		}
	}
	if len(meaningful) == 0 {
		return "Gram dashboard"
	}

	section := humanizeSlug(meaningful[0])
	if len(meaningful) == 1 {
		return section
	}
	return humanizeSlug(meaningful[len(meaningful)-1]) + " · " + section
}

// humanizeSlug turns a URL slug into title-cased words: "my-cool-toolset" →
// "My Cool Toolset".
func humanizeSlug(slug string) string {
	words := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || r == '_' })
	for i, word := range words {
		runes := []rune(word)
		words[i] = strings.ToUpper(string(runes[0])) + string(runes[1:])
	}
	return strings.Join(words, " ")
}

// escapeSlackText escapes the three characters Slack's mrkdwn treats as
// control characters in link labels.
// See https://docs.slack.dev/messaging/formatting-message-text#escaping.
func escapeSlackText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
