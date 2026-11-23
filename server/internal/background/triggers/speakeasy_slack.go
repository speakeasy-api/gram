package triggers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"net/http"
	"time"

	"github.com/google/uuid"

	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

// SpeakeasySlackTokens resolves the bot token of the Slack workspace whose
// Speakeasy app answers for a project.
type SpeakeasySlackTokens interface {
	ProjectBotToken(ctx context.Context, organizationID string, projectID uuid.UUID) (string, error)
}

// SetSpeakeasySlackTokens wires the token source for speakeasy_slack instances.
func (a *App) SetSpeakeasySlackTokens(tokens SpeakeasySlackTokens) {
	a.speakeasySlackTokens = tokens
}

// SpeakeasySlackConfig is the fixed config of a speakeasy_slack instance: the
// assistant answers when it is mentioned and in direct messages.
var SpeakeasySlackConfig = []byte(`{"routing":"mentions"}`)

// SlackSignature is how Slack signs requests: v0=HMAC-SHA256 over
// "v0:{timestamp}:{body}", with timestamps more than 5 minutes off rejected.
var SlackSignature = HMACScheme{
	NewHash:         func(key []byte) hash.Hash { return hmac.New(sha256.New, key) },
	Header:          "X-Slack-Signature",
	Encoding:        "hex",
	Prefix:          "v0=",
	Template:        "v0:{timestamp}:{body}",
	TimestampHeader: "X-Slack-Request-Timestamp",
	TimestampSkew:   5 * time.Minute,
}

// isSlackDefinition reports whether slug carries Slack events, from a
// customer's own Slack app or from the Speakeasy app.
func isSlackDefinition(slug string) bool {
	return slug == DefinitionSlugSlack || slug == DefinitionSlugSpeakeasySlack
}

// newSpeakeasySlackDefinition decodes Slack events exactly like the Slack
// definition. It is direct-kind so it stays out of the trigger catalog and
// trigger lists, and has no environment: the Speakeasy app's endpoint verifies
// each request with the app's signing secret before routing it here.
func newSpeakeasySlackDefinition() Definition {
	definition := newSlackDefinition()
	definition.Slug = DefinitionSlugSpeakeasySlack
	definition.Title = "Speakeasy Slack app"
	definition.Description = "Mentions and direct messages sent to the Speakeasy Slack app."
	definition.Kind = KindDirect
	definition.EnvRequirements = []EnvRequirement{}
	definition.AuthenticateWebhook = nil
	return definition
}

// SlackEventSummary is what the Speakeasy app's endpoint reads from a Slack
// Events API request before routing it.
type SlackEventSummary struct {
	// Type is the request type: "url_verification" or "event_callback".
	Type string

	// Challenge is set on url_verification requests.
	Challenge string

	TeamID    string
	EventType string
	ChannelID string

	// ThreadTS is the thread the event belongs to: its parent's timestamp, or
	// its own when it starts a thread.
	ThreadTS string
	UserID   string
	BotID    string
	Subtype  string
}

// Addressed reports whether a person sent the event to the app: a mention or a
// post in a direct message. Edits, deletions, and bot posts are not.
func (s SlackEventSummary) Addressed() bool {
	if s.UserID == "" || s.BotID != "" {
		return false
	}
	switch s.EventType {
	case "app_mention":
		return true
	case "message":
		return len(s.ChannelID) > 0 && s.ChannelID[0] == 'D' && (s.Subtype == "" || s.Subtype == "file_share" || s.Subtype == "thread_broadcast")
	default:
		return false
	}
}

// SummarizeSlackEvent reads a Slack Events API request body.
func SummarizeSlackEvent(body []byte) (SlackEventSummary, error) {
	var req slackEventRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return SlackEventSummary{}, fmt.Errorf("decode slack request: %w", err)
	}
	summary := SlackEventSummary{Type: req.Type, Challenge: req.Challenge, TeamID: req.TeamID, EventType: "", ChannelID: "", ThreadTS: "", UserID: "", BotID: "", Subtype: ""}
	if len(req.Event) == 0 {
		return summary, nil
	}
	event, err := decodeSlackEvent(req.Event)
	if err != nil {
		return SlackEventSummary{}, fmt.Errorf("decode slack event: %w", err)
	}
	summary.EventType = event.Type
	summary.ChannelID = event.Channel
	summary.ThreadTS = event.ThreadTs
	if summary.ThreadTS == "" {
		summary.ThreadTS = event.Ts
	}
	summary.UserID = event.User
	summary.BotID = event.BotID
	summary.Subtype = event.Subtype
	return summary, nil
}

// ProcessSpeakeasySlack dispatches a Slack event the Speakeasy app's endpoint
// has already verified and routed to instance, using botToken for the
// workspace's Slack calls.
func (a *App) ProcessSpeakeasySlack(ctx context.Context, instance triggerrepo.TriggerInstance, body []byte, headers http.Header, botToken string) (*WebhookIngressResult, error) {
	if instance.DefinitionSlug != DefinitionSlugSpeakeasySlack {
		return nil, fmt.Errorf("trigger instance %s is not a Speakeasy Slack app trigger", instance.ID)
	}
	rawConfig, err := configJSONToMap(instance.ConfigJson)
	if err != nil {
		return nil, fmt.Errorf("decode trigger config: %w", err)
	}
	definition, config, err := a.validateInstance(ctx, instance.ProjectID, uuid.Nil, instance.DefinitionSlug, rawConfig)
	if err != nil {
		return nil, fmt.Errorf("validate trigger instance: %w", err)
	}
	return a.ingestWebhook(ctx, instance, definition, config, map[string]string{"SLACK_BOT_TOKEN": botToken}, body, headers)
}

// speakeasySlackToken returns the bot token a speakeasy_slack instance posts
// with, or "" when the workspace cannot be used.
func (a *App) speakeasySlackToken(ctx context.Context, instance triggerrepo.TriggerInstance) (string, error) {
	if a.speakeasySlackTokens == nil {
		return "", nil
	}
	token, err := a.speakeasySlackTokens.ProjectBotToken(ctx, instance.OrganizationID, instance.ProjectID)
	if err != nil {
		return "", fmt.Errorf("resolve Speakeasy Slack app token: %w", err)
	}
	return token, nil
}

// ErrSpeakeasySlackUnrouted means a Slack event has no project to go to.
var ErrSpeakeasySlackUnrouted = errors.New("speakeasy slack event is not routed")
