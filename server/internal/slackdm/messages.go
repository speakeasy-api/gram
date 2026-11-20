// Package slackdm is the Slack plumbing shared by features that DM people
// through the Speakeasy Slack app and take their clicks back: finding a
// member's Slack account, posting and updating DMs, verifying Slack's request
// signature, replying through response_url, and routing clicks on the
// interactivity endpoint by action ID. Cards, recipients, message tables and
// decisions stay with each feature.
package slackdm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
)

// Messenger posts and updates DMs and finds the Slack accounts they go to.
type Messenger struct {
	db    repo.DBTX
	slack *slackapi.Client
}

// NewMessenger builds a Messenger that reads Slack account mappings from db
// and calls Slack through slack.
func NewMessenger(db repo.DBTX, slack *slackapi.Client) *Messenger {
	return &Messenger{db: db, slack: slack}
}

// Person is a Speakeasy member a DM goes to.
type Person struct {
	// UserID is the member's Speakeasy user ID.
	UserID string

	// Email is the member's email, used to find them in Slack when no
	// mapping is confirmed. Empty skips the email lookup.
	Email string
}

// FindUser finds person in a workspace the app can post to: a confirmed
// mapping first, then their email, when Slack has confirmed it the way
// directory sync requires. It returns a nil workspace when the person has no
// Slack account there, and a retryable error when a lookup failed in a way
// worth trying again.
func (m *Messenger) FindUser(ctx context.Context, organizationID string, person Person, workspaces []slackdirectoryconnections.MessagingWorkspace) (*slackdirectoryconnections.MessagingWorkspace, string, error) {
	accounts, err := slackdirectoryconnections.SlackAccounts(ctx, m.db, organizationID, person.UserID)
	if err != nil {
		return nil, "", fmt.Errorf("list recipient Slack accounts: %w", err)
	}
	for _, account := range accounts {
		for i := range workspaces {
			if workspaces[i].TeamID == account.TeamID {
				return &workspaces[i], account.UserID, nil
			}
		}
	}
	if person.Email == "" {
		return nil, "", nil
	}
	var retry error
	for i := range workspaces {
		body, err := m.slack.CallWithToken(ctx, "users.lookupByEmail", map[string]any{"email": person.Email}, workspaces[i].Token)
		if err != nil {
			if Retryable(err) {
				retry = errors.Join(retry, err)
			}
			continue
		}
		var found struct {
			User struct {
				ID             string `json:"id"`
				EmailConfirmed *bool  `json:"is_email_confirmed"`
			} `json:"user"`
		}
		if err := json.Unmarshal(body, &found); err != nil || found.User.ID == "" || !conv.PtrValOr(found.User.EmailConfirmed, true) {
			continue
		}
		return &workspaces[i], found.User.ID, nil
	}
	return nil, "", retry
}

// Posted is where chat.postMessage put a message.
type Posted struct {
	// Channel is the DM channel ID.
	Channel string `json:"channel"`

	// TS is the message timestamp, its ID within the channel.
	TS string `json:"ts"`
}

// Post sends a message to a Slack user. A user ID as the channel lands in the
// app's DM with them, so nobody has to invite the app anywhere.
func (m *Messenger) Post(ctx context.Context, token, slackUserID string, message Message) (Posted, error) {
	body, err := m.slack.CallWithToken(ctx, "chat.postMessage", map[string]any{
		"channel":      slackUserID,
		"text":         message.Text,
		"blocks":       message.Blocks,
		"unfurl_links": false,
	}, token)
	if err != nil {
		return Posted{Channel: "", TS: ""}, fmt.Errorf("post Slack DM: %w", err)
	}
	var posted Posted
	if err := json.Unmarshal(body, &posted); err != nil || posted.Channel == "" || posted.TS == "" {
		return Posted{Channel: "", TS: ""}, errors.New("post Slack DM: Slack returned no message")
	}
	return posted, nil
}

// Update replaces a message the app posted.
func (m *Messenger) Update(ctx context.Context, token, channel, ts string, message Message) error {
	_, err := m.slack.CallWithToken(ctx, "chat.update", map[string]any{
		"channel": channel,
		"ts":      ts,
		"text":    message.Text,
		"blocks":  message.Blocks,
	}, token)
	if err != nil {
		return fmt.Errorf("update Slack DM: %w", err)
	}
	return nil
}

// Retryable reports a Slack failure worth trying again: Slack's own errors,
// throttling and transport failures. A refusal the app's install or the
// request has to fix, such as a missing scope, is not.
func Retryable(err error) bool {
	if slackErr, ok := errors.AsType[*slackapi.Error](err); ok {
		return !slackErr.ClientFault()
	}
	return true
}

// TimeZone returns the IANA time zone on a Slack user's profile, or "" when
// the profile has none.
func (m *Messenger) TimeZone(ctx context.Context, token, slackUserID string) (string, error) {
	body, err := m.slack.CallWithToken(ctx, "users.info", map[string]any{"user": slackUserID}, token)
	if err != nil {
		return "", fmt.Errorf("get Slack user: %w", err)
	}
	var found struct {
		User struct {
			TZ string `json:"tz"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &found); err != nil {
		return "", fmt.Errorf("read Slack user: %w", err)
	}
	return found.User.TZ, nil
}
