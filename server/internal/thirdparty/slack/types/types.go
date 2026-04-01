package types

import (
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

// SlackEvent represents the top-level Slack event callback payload
// See: https://api.slack.com/events-api#receiving_events
type SlackEvent struct {
	Token          string          `json:"token"`
	Challenge      string          `json:"challenge,omitempty"`
	TeamID         string          `json:"team_id"`
	APIAppID       string          `json:"api_app_id"`
	Event          SlackInnerEvent `json:"event"`
	Type           string          `json:"type"`
	EventID        string          `json:"event_id"`
	EventTime      int64           `json:"event_time"`
	Authorizations []Authorization `json:"authorizations"`
	EventContext   string          `json:"event_context"`

	// GramAppID is the Gram-internal slack_apps.id, set by the event handler
	// before dispatching to Temporal. Not part of the Slack JSON payload.
	GramAppID string `json:"gram_app_id,omitempty"`
}

type SlackInnerEvent struct {
	Type        string `json:"type"`
	Channel     string `json:"channel"`
	User        string `json:"user"`
	Text        string `json:"text"`
	Ts          string `json:"ts"`
	ThreadTs    string `json:"thread_ts"`
	EventTs     string `json:"event_ts"`
	ChannelType string `json:"channel_type"`
}

type Authorization struct {
	EnterpriseID        *string `json:"enterprise_id"`
	TeamID              string  `json:"team_id"`
	UserID              string  `json:"user_id"`
	IsBot               bool    `json:"is_bot"`
	IsEnterpriseInstall bool    `json:"is_enterprise_install"`
}

const (
	AppMentionedThreadCacheExpiry = 24 * time.Hour
	SlackTokenCacheExpiry         = 30 * time.Minute
)

// SlackRegistrationToken is cached in Redis to hold in-flight registration tokens.
// The token is generated when an unmapped Slack user triggers an event, and consumed
// when they complete registration via the dashboard.
var _ cache.CacheableObject[SlackRegistrationToken] = (*SlackRegistrationToken)(nil)

type SlackRegistrationToken struct {
	Token          string `json:"token"`
	SlackAppID     string `json:"slack_app_id"`
	SlackAccountID string `json:"slack_account_id"`
	ChannelID      string `json:"channel_id"`
}

func SlackTokenCacheKey(token string) string {
	return "slack_token:" + token
}

func (t SlackRegistrationToken) CacheKey() string {
	return SlackTokenCacheKey(t.Token)
}

func (t SlackRegistrationToken) AdditionalCacheKeys() []string {
	return []string{}
}

func (t SlackRegistrationToken) TTL() time.Duration {
	return SlackTokenCacheExpiry
}

var _ cache.CacheableObject[AppMentionedThreads] = (*AppMentionedThreads)(nil)

type AppMentionedThreads struct {
	TeamID   string
	Channel  string
	ThreadTs string
}

func AppMentionedThreadsCacheKey(teamID, channel, threadTs string) string {
	return "appMetionedThreads:teamID-" + teamID + "-channel-" + channel + "-threadTs-" + threadTs
}

func (c AppMentionedThreads) CacheKey() string {
	return AppMentionedThreadsCacheKey(c.TeamID, c.Channel, c.ThreadTs)
}

func (c AppMentionedThreads) AdditionalCacheKeys() []string {
	return []string{}
}

func (c AppMentionedThreads) TTL() time.Duration {
	return AppMentionedThreadCacheExpiry
}
